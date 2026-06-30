//
// Copyright (c) 2017-2023 Pantacor Ltd.
//

package exports

import (
	"archive/tar"
	"bytes"
	"testing"
	"time"

	"gitlab.com/pantacor/pantahub-base/objects"
)

func TestTarEntrySize(t *testing.T) {
	cases := []struct {
		content int64
		want    int64
	}{
		{0, 512},        // header only, no content blocks
		{1, 1024},       // header + one padded block
		{512, 1024},     // header + exactly one block
		{513, 1536},     // header + two blocks
		{1024, 1536},    // header + two blocks
	}
	for _, tc := range cases {
		if got := tarEntrySize(tc.content); got != tc.want {
			t.Errorf("tarEntrySize(%d) = %d, want %d", tc.content, got, tc.want)
		}
	}
}

// TestBuildExportMetaMatchesRealTar verifies the cheap size estimate equals the
// actual uncompressed tar size produced by archive/tar for the same inputs.
func TestBuildExportMetaMatchesRealTar(t *testing.T) {
	state := []byte(`{"#spec":"pantavisor-service-system@1","example":"state"}`)
	objs := []objects.ObjectWithAccess{
		{Object: objects.Object{ID: "aaa", SizeInt: 100}},
		{Object: objects.Object{ID: "bbb", SizeInt: 4096}},
		{Object: objects.Object{ID: "ccc", SizeInt: 1}},
	}

	meta := buildExportMeta("7", state, objs)
	if meta.ObjectCount != len(objs) {
		t.Errorf("ObjectCount = %d, want %d", meta.ObjectCount, len(objs))
	}
	if meta.Rev != "7" {
		t.Errorf("Rev = %q, want %q", meta.Rev, "7")
	}

	// Build a real uncompressed tar with the same entry sizes and compare.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	writeEntry := func(name string, size int64) {
		hdr := &tar.Header{Name: name, Size: size, Mode: 0600, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(bytes.Repeat([]byte{0}, int(size))); err != nil {
			t.Fatal(err)
		}
	}
	writeEntry("json", int64(len(state)))
	for _, o := range objs {
		writeEntry("objects/"+o.ID, o.SizeInt)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if meta.UncompressedSize != int64(buf.Len()) {
		t.Errorf("UncompressedSize = %d, real tar = %d", meta.UncompressedSize, buf.Len())
	}
}
