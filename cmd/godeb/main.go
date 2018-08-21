// Copyright 2013-2014 Canonical Ltd.

// godeb dynamically translates stock upstream Go tarballs to deb packages.
//
// For details of how this tool works and context for why it was built,
// please refer to the following blog post:
//
//   http://blog.labix.org/2013/06/15/in-flight-deb-packages-of-go
//
package main

import (
	"bytes"
	"fmt"
	"go/build"
	"gopkg.in/xmlpath.v1"
	"io/ioutil"
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
			var urls []string
			for _, source := range tarballSources {
				urls = append(urls, source.url)
			}
			return fmt.Errorf("version %s not available at %s", version, strings.Join(urls, " or "))
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

	debName := fmt.Sprintf("go_%s_%s.deb", debVersion(version), debArch())
	deb, err := os.Create(debName + ".inprogress")
	if err != nil {
		return fmt.Errorf("cannot create deb: %v", err)
	}
	defer deb.Close()

	if err := createDeb(version, resp.Body, deb); err != nil {
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

type tarballSource struct {
	url, xpath string
}

var tarballSources = []tarballSource{
	{"https://golang.org/dl/", "//a[@class='download']/@href"},
}

func tarballs() ([]*Tarball, error) {
	type result struct {
		tarballs []*Tarball
		err      error
	}
	results := make(chan result)
	for _, source := range tarballSources {
		source := source
		go func() {
			tbs, err := tarballsFrom(source)
			results <- result{tbs, err}
		}()
	}

	var tbs []*Tarball
	var err error
	for _ = range tarballSources {
		r := <-results
		if r.err != nil {
			err = r.err
		} else {
			tbs = append(tbs, r.tarballs...)
		}
	}
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(tarballSlice(tbs)))
	return tbs, nil
}

func tarballsFrom(source tarballSource) ([]*Tarball, error) {
	resp, err := http.Get(source.url)
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
	var tbs []*Tarball
	iter := xmlpath.MustCompile(source.xpath).Iter(root)
	seen := make(map[string]bool)
	for iter.Next() {
		s := iter.Node().String()
		if strings.HasPrefix(s, "//") {
			s = "https:" + s
		}
		if strings.HasPrefix(s, "/dl/") {
			s = source.url + s[4:]
		}
		if tb, ok := parseURL(s); ok && !seen[tb.Version] {
			seen[tb.Version] = true
			tbs = append(tbs, tb)
		}
	}
	if len(tbs) == 0 {
		return nil, fmt.Errorf("no downloads available at " + source.url)
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
