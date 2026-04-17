package testgen

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateStorageFormat_Deterministic(t *testing.T) {
	cfg := DocConfig{Seed: 42, Sections: 3, Complexity: 1}
	doc1 := GenerateStorageFormat(cfg)
	doc2 := GenerateStorageFormat(cfg)
	assert.Equal(t, doc1, doc2, "same seed+config must produce identical output")
}

func TestGenerateStorageFormat_DifferentSeeds(t *testing.T) {
	doc1 := GenerateStorageFormat(DocConfig{Seed: 1, Sections: 3, Complexity: 1})
	doc2 := GenerateStorageFormat(DocConfig{Seed: 2, Sections: 3, Complexity: 1})
	assert.NotEqual(t, doc1, doc2, "different seeds must produce different output")
}

func TestGenerateStorageFormat_Level1_Structure(t *testing.T) {
	doc := GenerateStorageFormat(DocConfig{Seed: 42, Sections: 5, Complexity: 1})
	fp := FingerprintStorageFormat(doc)

	assert.Equal(t, 1, fp.HeadingCount[0], "expected one h1 (document title)")
	assert.Equal(t, 5, fp.HeadingCount[1], "expected 5 h2 sections")
	assert.Greater(t, fp.TextLength, 0)
	assert.Greater(t, fp.BoldCount, 0, "level 1 includes bold")
	assert.Greater(t, fp.ItalicCount, 0, "level 1 includes italic")
	assert.Greater(t, fp.ListItemCount, 0, "level 1 includes bullet lists")
}

func TestGenerateStorageFormat_ValidXHTML(t *testing.T) {
	doc := GenerateStorageFormat(DocConfig{Seed: 42, Sections: 3, Complexity: 1})
	assert.Contains(t, doc, "<h1>")
	assert.Contains(t, doc, "</h1>")
	assert.Contains(t, doc, "<h2>")
	assert.Contains(t, doc, "<p>")
	assert.Contains(t, doc, "</p>")
	assert.Equal(t,
		strings.Count(doc, "<h2>"),
		strings.Count(doc, "</h2>"),
	)
}

func TestGenerateStorageFormat_KnownChecksum_Level1(t *testing.T) {
	doc := GenerateStorageFormat(DocConfig{Seed: 42, Sections: 3, Complexity: 1})
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(doc)))
	t.Logf("SHA256: %s", hash)
	assert.Equal(t, "658abab57ebd10d0b6129f29d7eb80b86f24275a193dc615cdfc1134fa88d2c9", hash)
}

func TestGenerateStorageFormat_Level2_Structure(t *testing.T) {
	doc := GenerateStorageFormat(DocConfig{Seed: 42, Sections: 3, Complexity: 2})
	fp := FingerprintStorageFormat(doc)

	assert.Greater(t, fp.CodeBlockCount, 0, "level 2 includes code blocks")
	assert.Greater(t, fp.LinkCount, 0, "level 2 includes links")
	assert.Contains(t, doc, "<table>")
	assert.Contains(t, doc, "<ol>")
}

func TestGenerateStorageFormat_Level3_Structure(t *testing.T) {
	doc := GenerateStorageFormat(DocConfig{Seed: 42, Sections: 3, Complexity: 3})

	assert.Contains(t, doc, `ac:name="info"`, "level 3 includes info panels")
	assert.Contains(t, doc, `ac:name="expand"`, "level 3 includes expand macros")
	assert.Contains(t, doc, `ac:name="status"`, "level 3 includes status macros")
	assert.Contains(t, doc, `<ac:image>`, "level 3 includes images")
	assert.Contains(t, doc, `ri:user`, "level 3 includes user mentions")
}

func TestGenerateStorageFormat_KnownChecksum_Level2(t *testing.T) {
	doc := GenerateStorageFormat(DocConfig{Seed: 42, Sections: 3, Complexity: 2})
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(doc)))
	t.Logf("SHA256 level 2: %s", hash)
	assert.Equal(t, "b041d0594f5a8df700b0b2704366201d7c4bdea71b260b59a8195078b48b887e", hash)
}

func TestGenerateStorageFormat_KnownChecksum_Level3(t *testing.T) {
	doc := GenerateStorageFormat(DocConfig{Seed: 42, Sections: 3, Complexity: 3})
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(doc)))
	t.Logf("SHA256 level 3: %s", hash)
	assert.Equal(t, "0819fc74f58d69ee07873c39bae73c030e4e595c74afad57bf00ba4fbe0745da", hash)
}

func TestGenerateStorageFormat_LargeDocument(t *testing.T) {
	doc := GenerateStorageFormat(DocConfig{Seed: 99, Sections: 30, Complexity: 2, TargetBytes: 50_000})

	assert.Greater(t, len(doc), 50_000, "document should meet target size")
	fp := FingerprintStorageFormat(doc)
	assert.GreaterOrEqual(t, fp.HeadingCount[1], 30, "at least 30 h2 sections")
	assert.Greater(t, fp.TextLength, 10_000, "substantial text content")

	// Deterministic
	doc2 := GenerateStorageFormat(DocConfig{Seed: 99, Sections: 30, Complexity: 2, TargetBytes: 50_000})
	assert.Equal(t, doc, doc2)
}

func TestGenerateStorageFormat_KnownChecksum_Large(t *testing.T) {
	doc := GenerateStorageFormat(DocConfig{Seed: 99, Sections: 30, Complexity: 2, TargetBytes: 50_000})
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(doc)))
	t.Logf("SHA256 large: %s", hash)
	assert.Equal(t, "00a759e8aad45bc2ba10598caa7afdf04724e457c7eace7ecfa45693073e7da7", hash)
}
