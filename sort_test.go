package main

import (
	. "launchpad.net/gocheck"
	"math/rand"
	"sort"
	"testing"
)

func Test(t *testing.T) {
	TestingT(t)
}

type S struct{}

var _ = Suite(S{})

func (S) TestVersionOrder(c *C) {
	versions := []string{
		"1.1.1",
		"1.1rc3",
		"1.1rc2",
		"1.1rc1",
		"1.1beta2",
		"1.0.10",
		"1.0.3",
		"1.0.1",
		"1.0rc1",
		"1.0",
	}

	for perm := 0; perm < 1000; perm++ {
		vs := make([]string, len(versions))
		ts := make(tarballSlice, len(versions))
		pi := rand.Perm(len(vs))
		for i := range versions {
			ts[i] = &Tarball{Version: versions[pi[i]]}
		}
		sort.Sort(ts)
		for i := range versions {
			vs[i] = ts[i].Version
		}
		c.Assert(vs, DeepEquals, versions)
	}
}
