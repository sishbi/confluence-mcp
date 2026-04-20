package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/net/html"
)

func TestAnonymiseText_Deterministic(t *testing.T) {
	result1 := anonymiseText("Hello world")
	result2 := anonymiseText("Hello world")
	assert.Equal(t, result1, result2, "same input must produce same output")
	assert.NotEqual(t, "Hello world", result1, "text should be replaced")
}

func TestAnonymiseText_PreservesWhitespace(t *testing.T) {
	result := anonymiseText("  leading and trailing  ")
	assert.True(t, strings.HasPrefix(result, "  "), "should preserve leading whitespace")
	assert.True(t, strings.HasSuffix(result, "  "), "should preserve trailing whitespace")
}

func TestAnonymiseText_PreservesCapitalisation(t *testing.T) {
	result := anonymiseText("Hello world")
	first := strings.Fields(result)[0]
	assert.True(t, first[0] >= 'A' && first[0] <= 'Z', "first word should be capitalised")
}

func TestAnonymiseText_Empty(t *testing.T) {
	assert.Equal(t, "", anonymiseText(""))
	assert.Equal(t, "   ", anonymiseText("   "))
}

func TestAnonymiseText_PreservesWordCount(t *testing.T) {
	result := anonymiseText("one two three four five")
	assert.Len(t, strings.Fields(result), 5)
}

func TestLooksLikeURL(t *testing.T) {
	assert.True(t, looksLikeURL("https://example.com"))
	assert.True(t, looksLikeURL("http://example.com"))
	assert.True(t, looksLikeURL("//cdn.example.com/img.png"))
	assert.True(t, looksLikeURL("/wiki/spaces/DEV/pages/123"))
	assert.True(t, looksLikeURL("/download/attachments/123/file.pdf"))
	assert.False(t, looksLikeURL("just some text"))
	assert.False(t, looksLikeURL(""))
}

func TestAnonymiseURL_Deterministic(t *testing.T) {
	url := "https://company.atlassian.net/wiki/spaces/DEV/pages/123"
	result1 := anonymiseURL(url)
	result2 := anonymiseURL(url)
	assert.Equal(t, result1, result2)
}

func TestAnonymiseURL_PreservesScheme(t *testing.T) {
	result := anonymiseURL("https://company.atlassian.net/wiki/pages/123")
	assert.True(t, strings.HasPrefix(result, "https://example.com/"))
}

func TestAnonymiseURL_RemovesOriginalHostname(t *testing.T) {
	result := anonymiseURL("https://company.atlassian.net/wiki/pages/123")
	assert.NotContains(t, result, "company")
	assert.NotContains(t, result, "atlassian")
	assert.Contains(t, result, "example.com")
}

func TestAnonymiseURL_Passthrough(t *testing.T) {
	assert.Equal(t, "", anonymiseURL(""))
	assert.Equal(t, "#", anonymiseURL("#"))
}

func TestAnonymiseNode_SkipsCodeBlocks(t *testing.T) {
	doc, _ := html.Parse(strings.NewReader("<div><p>visible</p><code>secret code</code></div>"))
	anonymiseNode(doc)
	var sb strings.Builder
	_ = html.Render(&sb, doc)
	output := sb.String()
	assert.Contains(t, output, "secret code", "code block content should be preserved")
	assert.NotContains(t, output, "visible", "paragraph text should be anonymised")
}

func TestAnonymiseNode_AnonymisesURLAttributes(t *testing.T) {
	doc, _ := html.Parse(strings.NewReader(`<div><a href="https://company.atlassian.net/page" title="Company Page">link</a></div>`))
	anonymiseNode(doc)
	var sb strings.Builder
	_ = html.Render(&sb, doc)
	output := sb.String()
	assert.NotContains(t, output, "company.atlassian.net", "href should be anonymised")
	assert.NotContains(t, output, "Company Page", "title should be anonymised")
	assert.Contains(t, output, "example.com", "URL should use example.com")
}

func TestAnonymiseNode_PreservesStructuralAttrs(t *testing.T) {
	doc, _ := html.Parse(strings.NewReader(`<div class="wiki-content" id="main"><p>text</p></div>`))
	anonymiseNode(doc)
	var sb strings.Builder
	_ = html.Render(&sb, doc)
	output := sb.String()
	assert.Contains(t, output, `class="wiki-content"`, "class should be preserved")
	assert.Contains(t, output, `id="main"`, "id should be preserved")
}

func TestFindContentNode_ByID(t *testing.T) {
	doc, _ := html.Parse(strings.NewReader(`<html><body><div id="main-content"><p>found</p></div></body></html>`))
	node := findContentNode(doc)
	assert.NotNil(t, node)
	assert.Equal(t, "div", node.Data)
}

func TestFindContentNode_ByClass(t *testing.T) {
	doc, _ := html.Parse(strings.NewReader(`<html><body><div class="wiki-content"><p>found</p></div></body></html>`))
	node := findContentNode(doc)
	assert.NotNil(t, node)
}

func TestFindContentNode_FallsThrough(t *testing.T) {
	doc, _ := html.Parse(strings.NewReader(`<html><body><div><p>no match</p></div></body></html>`))
	node := findContentNode(doc)
	assert.Nil(t, node)
}

func TestFindBody(t *testing.T) {
	doc, _ := html.Parse(strings.NewReader(`<html><body><p>text</p></body></html>`))
	body := findBody(doc)
	assert.NotNil(t, body)
	assert.Equal(t, "body", body.Data)
}
