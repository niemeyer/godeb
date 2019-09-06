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
	"encoding/json"
	"fmt"
	"go/build"
	"net/http"
	"os"
	"os/exec"
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

type GolangDlFile struct {
	Arch     string `json:"arch"`
	Filename string `json:"filename"`
	Os       string `json:"os"`
	Version  string `json:"version"`
}

type GolangDlVersion struct {
	Version string         `json:"version"`
	Files   []GolangDlFile `json:"files"`
}

// REST API described in https://github.com/golang/website/blob/master/internal/dl/dl.go
func tarballs() ([]*Tarball, error) {
	url := "https://golang.org/dl/?mode=json&include=all"
	downloadBaseURL := "https://dl.google.com/go/"

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	var versions []GolangDlVersion
	err = json.NewDecoder(resp.Body).Decode(&versions)
	if err != nil {
		return nil, err
	}

	var tbs []*Tarball
	for _, v := range versions {
		for _, f := range v.Files {
			if f.Os == build.Default.GOOS && f.Arch == build.Default.GOARCH {
				t := Tarball{
					Version: strings.TrimPrefix(f.Version, "go"),
					URL:     downloadBaseURL + f.Filename}
				tbs = append(tbs, &t)
				break
			}
		}
	}

	sort.Sort(sort.Reverse(tarballSlice(tbs)))
	return tbs, nil
}
