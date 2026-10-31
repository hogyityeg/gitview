package main

import (
	"net"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

func TestStoreInstancesDoNotOverwriteEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	a, err := loadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := loadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.setEnabled("/a", "x", true, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.setEnabled("/b", "y", true, nil); err != nil {
		t.Fatal(err)
	}
	c, err := loadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.enabled("/a", nil); !slices.Equal(got, []string{"x"}) {
		t.Errorf("/a = %v, want [x]", got)
	}
	if got := c.enabled("/b", nil); !slices.Equal(got, []string{"y"}) {
		t.Errorf("/b = %v, want [y]", got)
	}
}

func TestStoreDefaultsToAllRepos(t *testing.T) {
	s, err := loadStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	all := []string{"b", "a", "c"}
	if got := s.enabled("/r", all); !slices.Equal(got, all) {
		t.Errorf("unsaved folder = %v, want all %v", got, all)
	}
	if err := s.setEnabled("/r", "b", false, all); err != nil {
		t.Fatal(err)
	}
	if got := s.enabled("/r", all); !slices.Equal(got, []string{"a", "c"}) {
		t.Errorf("after hiding b = %v, want [a c]", got)
	}
}

func TestListenDefaultSkipsTakenPort(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	port := taken.Addr().(*net.TCPAddr).Port

	lns, err := listenAll([]string{"127.0.0.1"}, strconv.Itoa(port))
	if err == nil {
		lns[0].Close()
		t.Fatal("listening on a taken port succeeded")
	}
	lns, err = listenDefault(port, port+20)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, l := range lns {
			l.Close()
		}
	}()
	if got := lns[0].Addr().(*net.TCPAddr).Port; got == port {
		t.Errorf("got the taken port %d", got)
	}
}
