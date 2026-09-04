package rule

import (
	"sync"

	"github.com/microsoft/typescript-go/shim/core"
	"github.com/microsoft/typescript-go/shim/vfs"
	"github.com/web-infra-dev/rslint/internal/vue/vuesfc"
)

// Component lazily provides the Vue single file component structure behind one
// linted file.
//
// A component's parsed text is a projection of its <script> blocks with every
// other byte blanked, so `ctx.SourceFile.Text()` cannot answer anything about
// the component itself: the template, the styles and even the block tags are
// gone from it by the time a rule runs. The component's own text is read back
// from the file system instead, exactly as [SourceBOM] reads a file's bytes for
// the one question its text cannot answer.
//
// One store per file is shared by every rule on it, and the read happens on the
// first question asked. A file that is not a component, and a component no Vue
// rule asks about, both pay nothing.
type Component struct {
	fs   vfs.FS
	path string
	once sync.Once
	// text is the component's own text, markup and all. It is deliberately
	// not vuesfc.Result.Text, which is the blanked projection the parser
	// reads: a template rule asking for the component would get spaces.
	text string
	sfc  vuesfc.Result
	ok   bool
}

// NewComponent returns the store for one file. A nil file system, or a path
// that is not a component, yields a store that reports it is not one.
func NewComponent(fileSystem vfs.FS, path string) *Component {
	return &Component{fs: fileSystem, path: path}
}

func (c *Component) load() {
	if c == nil {
		return
	}
	c.once.Do(func() {
		if c.fs == nil || !vuesfc.IsFile(c.path) {
			return
		}
		text, ok := c.fs.ReadFile(c.path)
		if !ok {
			return
		}
		c.text = text
		c.sfc = vuesfc.Extract(text)
		c.ok = true
	})
}

// IsComponent reports whether this file is a Vue single file component whose
// text could be read. A rule that only makes sense inside one returns no
// listeners when this is false.
func (c *Component) IsComponent() bool {
	c.load()
	return c != nil && c.ok
}

// Text returns the component's own text — markup and all — or the empty string
// for a file that is not one. Offsets into it are the offsets a rule already
// holds, because the projection the parser read preserves every one of them.
func (c *Component) Text() string {
	c.load()
	if c == nil || !c.ok {
		return ""
	}
	return c.text
}

// Blocks returns every top-level block of the component in source order.
// Treat the result as read-only.
func (c *Component) Blocks() []vuesfc.Block {
	c.load()
	if c == nil || !c.ok {
		return nil
	}
	return c.sfc.Blocks
}

// ScriptSetupRange returns the content range of the component's `<script setup>`
// block. A component with no such block reports false.
//
// This is what makes a `<script setup>` rule expressible: the parsed text holds
// both script blocks at once, so a rule that must apply to one and not the
// other has to ask which range it is looking at.
func (c *Component) ScriptSetupRange() (core.TextRange, bool) {
	c.load()
	if c == nil || !c.ok {
		return core.TextRange{}, false
	}
	for _, block := range c.sfc.Blocks {
		if block.Kind == vuesfc.BlockScript && block.Setup {
			return block.Content, true
		}
	}
	return core.TextRange{}, false
}

// InScriptSetup reports whether a position in the file falls inside the
// component's `<script setup>` block.
func (c *Component) InScriptSetup(position int) bool {
	setup, ok := c.ScriptSetupRange()
	if !ok {
		return false
	}
	return position >= setup.Pos() && position < setup.End()
}

// TemplateRange returns the content range of the component's `<template>`
// block. A component with no template, or one whose template lives in another
// file through `src`, reports false.
func (c *Component) TemplateRange() (core.TextRange, bool) {
	c.load()
	if c == nil || !c.ok {
		return core.TextRange{}, false
	}
	for _, block := range c.sfc.Blocks {
		if block.Kind == vuesfc.BlockTemplate && !block.External && block.Content.Len() > 0 {
			return block.Content, true
		}
	}
	return core.TextRange{}, false
}
