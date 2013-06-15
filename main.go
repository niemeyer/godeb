package main

import (
	"fmt"
	"go/build"
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
`

func main() {
	if err := run(); err != nil {
		fmt.Println("error:", err)
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
		return downloadCommand(version, command == "install")
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

func downloadCommand(version string, install bool) error {
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
	if err := os.Rename(debName + ".inprogress", debName); err != nil {
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
	root, err := xmlpath.ParseHTML(resp.Body)
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
	return &Tarball{url, s[2:len(s)-len(suffix)]}, true
}
