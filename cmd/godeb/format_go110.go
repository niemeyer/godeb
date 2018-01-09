// +build go1.10

package main

import (
	"archive/tar"
)

func dropPax(h *tar.Header) {
	h.Format = tar.FormatGNU
	h.PAXRecords = nil
}

func init() {
	processTarHeader = dropPax
}
