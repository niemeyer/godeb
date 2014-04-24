// Copyright 2013-2014 Canonical Ltd.

package main

import (
	"bytes"
	"fmt"
	"go/build"
	"io"
	"io/ioutil"
	"launchpad.net/xmlpath"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
)

var usage = `Usage: godeb <command> [<options> ...]

Available commands:

    list
    install [<version>]
    download [<version>]
    remove
    fromtarball <tarball> <version>
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 2 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		fmt.Println(usage)
		return nil
	}
	if len(os.Args) < 2 {
		fmt.Println(usage)
		return fmt.Errorf("command missing")
	}
	if strings.HasPrefix(os.Args[1], "-") {
		return fmt.Errorf("unknown option: %s", os.Args[1])
	}

	switch command := os.Args[1]; command {
	case "list":
		if len(os.Args) > 2 {
			return fmt.Errorf("list command takes no arguments")
		}
		return listCommand()
	case "download", "install":
		version := ""
		if len(os.Args) == 3 {
			version = os.Args[2]
		} else if len(os.Args) > 3 {
			return fmt.Errorf("too many arguments to %s command", command)
		}
		return actionCommand(version, command == "install")
	case "remove":
		return removeCommand()
	case "fromtarball":
		if len(os.Args) != 4 {
			return fmt.Errorf("wrong number of arguments to fromtarball command")
		}
		tarballName := os.Args[2]
		version := os.Args[3]

		if !strings.Contains(tarballName, version+".") {
			fmt.Println(tarballName, version+".")
			return fmt.Errorf("Tarball does not appear to correspond to given version")
		}

		file, err := os.Open(tarballName)
		if err != nil {
			return fmt.Errorf("Unable to open tarball: %s", err.Error())
		}

		return fromTarball(version, file, true)
	default:
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
	return nil
}

func listCommand() error {
	tbs, err := tarballs()
	if err != nil {
		return err
	}
	for _, tb := range tbs {
		fmt.Println(tb.Version)
	}
	return nil
}

func removeCommand() error {
	args := []string{"dpkg", "--purge", "go"}
	if os.Getuid() != 0 {
		args = append([]string{"sudo"}, args...)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("while removing go package: %v", err)
	}
	return nil
}

func actionCommand(version string, install bool) error {
	tbs, err := tarballs()
	if err != nil {
		return err
	}
	var url string
	if version == "" {
		version = tbs[0].Version
		url = tbs[0].URL
	} else {
		for _, tb := range tbs {
			if version == tb.Version {
				url = tb.URL
				break
			}
		}
		if url == "" {
			return fmt.Errorf("version %s not availble at %s", version, downloadsURL)
		}
	}

	installed, err := installedDebVersion()
	if err == errNotInstalled {
		// that's okay
	} else if err != nil {
		return err
	} else if install && debVersion(version) == installed {
		return fmt.Errorf("go version %s is already installed", version)
	}

	fmt.Println("processing", url)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download %s: %v", url, err)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("got status code %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	return fromTarball(version, resp.Body, install)
}

func fromTarball(version string, tarball io.Reader, install bool) error {
	debName := fmt.Sprintf("go_%s_%s.deb", debVersion(version), debArch())
	deb, err := os.Create(debName + ".inprogress")
	if err != nil {
		return fmt.Errorf("cannot create deb: %v", err)
	}
	defer deb.Close()

	if err := createDeb(version, tarball, deb); err != nil {
		return err
	}
	if err := os.Rename(debName+".inprogress", debName); err != nil {
		return err
	}
	fmt.Println("package", debName, "ready")

	if install {
		args := []string{"dpkg", "-i", debName}
		if os.Getuid() != 0 {
			args = append([]string{"sudo"}, args...)
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("while installing go package: %v", err)
		}
	}
	return nil
}

type Tarball struct {
	URL     string
	Version string
}

const downloadsURL = "https://code.google.com/p/go/downloads/list?can=1&q=linux"

func tarballs() ([]*Tarball, error) {
	resp, err := http.Get(downloadsURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read http response: %v", err)
	}
	clearScripts(data)
	root, err := xmlpath.ParseHTML(bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	download := xmlpath.MustCompile("//a[@title='Download']/@href")

	var tbs []*Tarball
	iter := download.Iter(root)
	for iter.Next() {
		if tb, ok := parseURL("https:" + iter.Node().String()); ok {
			tbs = append(tbs, tb)
		}
	}
	sort.Sort(tarballSlice(tbs))
	if len(tbs) == 0 {
		return nil, fmt.Errorf("no downloads available at " + downloadsURL)
	}
	return tbs, nil
}

func parseURL(url string) (tb *Tarball, ok bool) {
	// url looks like https://.../go1.1beta2.linux-amd64.tar.gz
	_, s := path.Split(url)
	if len(s) < 3 || !strings.HasPrefix(s, "go") || !(s[2] >= '1' && s[2] <= '9') {
		return nil, false
	}
	suffix := fmt.Sprintf(".linux-%s.tar.gz", build.Default.GOARCH)
	if !strings.HasSuffix(s, suffix) {
		return nil, false
	}
	return &Tarball{url, s[2 : len(s)-len(suffix)]}, true
}

func clearScripts(data []byte) {
	startTag := []byte("<script")
	closeTag := []byte("</script>")
	var i, j int
	for {
		i = j + bytes.Index(data[j:], startTag)
		if i < j {
			break
		}
		i = i + bytes.IndexByte(data[i:], '>') + 1
		j = i + bytes.Index(data[i:], closeTag)
		for i < j {
			data[i] = ' '
			i++
		}
	}
}
