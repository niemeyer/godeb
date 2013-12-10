package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"fmt"
	"github.com/blakesmith/ar"
	"go/build"
	"io"
	"os/exec"
	"strings"
	"time"
)

func createDeb(version string, tarball io.Reader, deb io.Writer) error {
	now := time.Now()
	dataTarGz, md5sums, instSize, err := translateTarball(now, tarball)
	if err != nil {
		return err
	}
	controlTarGz, err := createControl(now, version, instSize, md5sums)
	if err != nil {
		return err
	}
	w := ar.NewWriter(deb)
	if err := w.WriteGlobalHeader(); err != nil {
		return fmt.Errorf("cannot write ar header to deb file: %v", err)
	}

	if err := addArFile(now, w, "debian-binary", []byte("2.0\n")); err != nil {
		return fmt.Errorf("cannot pack debian-binary: %v", err)
	}
	if err := addArFile(now, w, "control.tar.gz", controlTarGz); err != nil {
		return fmt.Errorf("cannot add control.tar.gz to deb: %v", err)
	}
	if err := addArFile(now, w, "data.tar.gz", dataTarGz); err != nil {
		return fmt.Errorf("cannot add data.tar.gz to deb: %v", err)
	}
	return nil
}

const control = `
Package: go
Version: %s
Architecture: %s
Maintainer: Gustavo Niemeyer <niemeyer@canonical.com>
Installed-Size: %d
Conflicts: golang, golang-stable, golang-tip, golang-weekly
Section: devel
Priority: extra
Homepage: http://golang.org
Description: Go language compiler and tools (gc)
 The Go programming language is an open source project to make programmers
 more productive. Go is expressive, concise, clean, and efficient.
 Its concurrency mechanisms make it easy to write programs that get the
 most out of multicore and networked machines, while its novel type system
 enables flexible and modular program construction. Go compiles quickly to
 machine code yet has the convenience of garbage collection and the power
 of run-time reflection. It's a fast, statically typed, compiled language
 that feels like a dynamically typed, interpreted language.
`

func debArch() string {
	arch := build.Default.GOARCH
	if arch == "386" {
		return "i386"
	}
	return arch
}

func isDigit(version string, i int) bool {
	return i >= 0 && i < len(version) && version[i] >= '0' && version[i] <= '9'
}

func debVersion(version string) string {
	for _, s := range []string{"rc", "beta"} {
		i := strings.Index(version, s)
		if isDigit(version, i-1) && isDigit(version, i+len(s)) {
			version = version[:i] + "~" + version[i:]
			break
		}
	}
	return version + "-godeb1"
}

var errNotInstalled = fmt.Errorf("package go is not installed")

func installedDebVersion() (string, error) {
	output, err := exec.Command("dpkg-query", "-f", "${db:Status-Abbrev}${source:Version}", "-W", "go").Output()
	if err != nil {
		return "", fmt.Errorf("while querying for installed go package version: %v", err)
	}
	s := string(output)
	if !strings.HasPrefix(s, "ii ") {
		return "", errNotInstalled
	}
	return s[3:], nil
}

func createControl(now time.Time, version string, instSize int64, md5sums []byte) (controlTarGz []byte, err error) {
	buf := &bytes.Buffer{}
	compress := gzip.NewWriter(buf)
	tarball := tar.NewWriter(compress)

	body := []byte(fmt.Sprintf(control, debVersion(version), debArch(), instSize/1024))
	hdr := tar.Header{
		Name: "control",
		Size: int64(len(body)),
		Mode: 0644,
		ModTime: now,
		Typeflag: tar.TypeReg,
	}
	if err := tarball.WriteHeader(&hdr); err != nil {
		return nil, fmt.Errorf("cannot write header of control file to control.tar.gz: %v", err)
	}
	if _, err := tarball.Write(body); err != nil {
		return nil, fmt.Errorf("cannot write control file to control.tar.gz: %v", err)
	}

	hdr = tar.Header{
		Name: "md5sums",
		Size: int64(len(md5sums)),
		Mode: 0644,
		ModTime: now,
		Typeflag: tar.TypeReg,
	}
	if err := tarball.WriteHeader(&hdr); err != nil {
		return nil, fmt.Errorf("cannot write header of md5sums file to control.tar.gz: %v", err)
	}
	if _, err := tarball.Write(md5sums); err != nil {
		return nil, fmt.Errorf("cannot write md5sums file to control.tar.gz: %v", err)
	}

	if err := tarball.Close(); err != nil {
		return nil, fmt.Errorf("closing control.tar.gz: %v", err)
	}
	if err := compress.Close(); err != nil {
		return nil, fmt.Errorf("closing control.tar.gz: %v", err)
	}
	return buf.Bytes(), nil
}

func addArFile(now time.Time, w *ar.Writer, name string, body []byte) error {
	hdr := ar.Header{
		Name: name,
		Size: int64(len(body)),
		Mode: 0644,
		ModTime: now,
	}
	if err := w.WriteHeader(&hdr); err != nil {
		return fmt.Errorf("cannot write file header: %v", err)
	}
	_, err := w.Write(body)
	return err
}

func translateTarball(now time.Time, tarball io.Reader) (dataTarGz, md5sums []byte, instSize int64, err error) {
	buf := &bytes.Buffer{}
	compress := gzip.NewWriter(buf)
	out := tar.NewWriter(compress)

	md5buf := &bytes.Buffer{}
	md5tmp := make([]byte, 0, md5.Size)

	uncompress, err := gzip.NewReader(tarball)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("cannot uncompress upstream tarball: %v", err)
	}
	in := tar.NewReader(uncompress)
	first := true
	for {
		h, err := in.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, 0, fmt.Errorf("cannot read upstream tarball: %v", err)
		}
		instSize += h.Size
		h.Name = strings.TrimLeft(h.Name, "./")
		if first && h.Name != "go" && h.Name != "go/" {
			h := tar.Header{
				Name: "./usr/local/go/",
				Mode: 0755,
				ModTime: h.ModTime,
				Typeflag: tar.TypeDir,
			}
			if err := out.WriteHeader(&h); err != nil {
				return nil, nil, 0, fmt.Errorf("cannot write header of %s to data.tar.gz: %v", h.Name,  err)
			}
		}
		if !strings.HasPrefix(h.Name, "go/") {
			return nil, nil, 0, fmt.Errorf("upstream tarball has file in unexpected path: %s", h.Name)
		}
		const prefix = "./usr/local/"
		h.Name = prefix + h.Name
		if h.Typeflag == tar.TypeDir && !strings.HasSuffix(h.Name, "/") {
			h.Name += "/"
		}
		if err := out.WriteHeader(h); err != nil {
			return nil, nil, 0, fmt.Errorf("cannot write header of %s to data.tar.gz: %v", h.Name,  err)
		}
		//fmt.Println("packing", h.Name[len(prefix):])
		if h.Typeflag == tar.TypeDir {
			continue
		}

		digest := md5.New()
		if _, err := io.Copy(out, io.TeeReader(in, digest)); err != nil {
			return nil, nil, 0, err
		}
		fmt.Fprintf(md5buf, "%x  %s\n", digest.Sum(md5tmp), h.Name[2:])
	}

	if err := addTarSymlink(now, out, "./usr/bin/go", "/usr/local/go/bin/go"); err != nil {
		return nil, nil, 0, err
	}
	if err := addTarSymlink(now, out, "./usr/bin/gofmt", "/usr/local/go/bin/gofmt"); err != nil {
		return nil, nil, 0, err
	}
	if err := addTarSymlink(now, out, "./usr/bin/godoc", "/usr/local/go/bin/godoc"); err != nil {
		return nil, nil, 0, err
	}

	if err := out.Close(); err != nil {
		return nil, nil, 0, err
	}
	if err := compress.Close(); err != nil {
		return nil, nil, 0, err
	}
	return buf.Bytes(), md5buf.Bytes(), instSize, nil
}

func addTarSymlink(now time.Time, out *tar.Writer, name, target string) error {
	h := tar.Header{
		Name: name,
		Linkname: target,
		Mode: 0777,
		ModTime: now,
		Typeflag: tar.TypeSymlink,
	}
	if err := out.WriteHeader(&h); err != nil {
		return fmt.Errorf("cannot write header of %s to data.tar.gz: %v", h.Name,  err)
	}
	return nil
}
