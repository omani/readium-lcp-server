// Copyright 2020 Readium Foundation. All rights reserved.
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE file exposed on Github (readium) in the project repository.

package pack

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/omani/readium-lcp-server/rwpm"
)

// TestMapW3CPublication tests the mapping of a W3C manifest to a Readium manifest
func TestMapW3CPublication(t *testing.T) {

	file, err := os.Open("./samples/w3cman1.json")
	if err != nil {
		t.Fatalf("Could not find the sample file, %s", err)
	}
	defer file.Close()

	var w3cManifest rwpm.W3CPublication
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&w3cManifest)
	if err != nil {
		t.Fatalf("Could not unmarshal sample file, %s", err)
	}

	rman := generateRWPManifest(w3cManifest)

	// metadata
	meta := rman.Metadata

	if meta.Identifier != "id1" {
		t.Fatalf("W3C Identifer badly mapped")
	}
	if meta.Title.Text() != "audiotest" {
		t.Fatalf("W3C Name badly mapped")
	}
	if meta.Publisher.Name() != "Stanford" {
		t.Fatalf("W3C Publisher badly mapped")
	}
	if meta.Author.Name() != "" {
		t.Fatalf("W3C Author badly mapped 1")
	}

	i := 0
	for _, a := range meta.Author {
		if a.Name.Text() == "Alpha" || a.Name.Text() == "Beta" || a.Name.Text() == "Gamma" {
			i++
		}
	}
	if i != 2 {
		t.Fatalf("W3C Author badly mapped, expected 2 got %d", i)
	}
	if meta.Language[0] != "fr" || meta.Language[1] != "en" {
		t.Fatalf("W3C InLanguage badly mapped")
	}
	if *meta.Published != rwpm.Date(time.Date(2020, 03, 23, 12, 50, 20, 0, time.UTC)) {
		t.Fatalf("W3C DatePublished badly mapped")
	}
	mod := time.Date(2020, 03, 23, 16, 58, 27, 372000000, time.UTC)
	if *meta.Modified != mod {
		t.Fatalf("W3C DateModified badly mapped")
	}
	if meta.Duration != 150 {
		t.Fatalf("W3C Duration badly mapped")
	}

	// Linked resources
	item0 := rman.ReadingOrder[0]
	if item0.Href != "audio/gtr-jazz.mp3" {
		t.Fatalf("W3C URL badly mapped")
	}
	if item0.Type != "audio/mpeg" {
		t.Fatalf("W3C EncodingFormat badly mapped")
	}
	if item0.Title != "Track 1" {
		t.Fatalf("W3C Name badly mapped")
	}
	if item0.Duration != 10 {
		t.Fatalf("W3C Duration badly mapped")
	}

	item1 := rman.ReadingOrder[1]
	if item1.Type != "audio/mpeg" {
		t.Fatalf("W3C EncodingFormat badly mapped if missing")
	}
	if item1.Alternate[0].Href != "audio/Latin.mp3" {
		t.Fatalf("W3C Name badly mapped in Alternate")
	}
	if item1.Alternate[0].Type != "audio/mpeg" {
		t.Fatalf("W3C EncodingFormat badly mapped in Alternate")
	}

}
