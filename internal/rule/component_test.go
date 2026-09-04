package rule

import (
	"testing"

	"github.com/microsoft/typescript-go/shim/vfs"
	"github.com/web-infra-dev/rslint/internal/utils"
)

// countingFS records how many times a path's bytes were read, which is what
// "costs nothing" has to mean for a store that exists to avoid reads.
type countingFS struct {
	vfs.FS
	reads int
}

func (f *countingFS) ReadFile(path string) (string, bool) {
	f.reads++
	return f.FS.ReadFile(path)
}

func newComponentFS(t *testing.T, path, contents string) *countingFS {
	t.Helper()
	return &countingFS{FS: utils.NewOverlayVFSForFile(path, contents)}
}

const componentFixture = "<template>\n  <div/>\n</template>\n\n" +
	"<script>\nexport const shared = 1;\n</script>\n\n" +
	"<script setup>\nconst local = 2;\n</script>\n"

// TestComponentReadsNothingUnlessAsked locks in the claim the whole design
// rests on for performance: a component's bytes are read only when a rule
// actually asks about the component, and then only once for every rule on that
// file.
func TestComponentReadsNothingUnlessAsked(t *testing.T) {
	t.Parallel()

	t.Run("a store nobody asks reads nothing", func(t *testing.T) {
		t.Parallel()
		fileSystem := newComponentFS(t, "/App.vue", componentFixture)
		NewComponent(fileSystem, "/App.vue")

		if fileSystem.reads != 0 {
			t.Errorf("read the component %d times without being asked", fileSystem.reads)
		}
	})

	t.Run("many questions read once", func(t *testing.T) {
		t.Parallel()
		fileSystem := newComponentFS(t, "/App.vue", componentFixture)
		component := NewComponent(fileSystem, "/App.vue")

		component.IsComponent()
		component.Blocks()
		component.Text()
		component.ScriptSetupRange()
		component.TemplateRange()

		if fileSystem.reads != 1 {
			t.Errorf("read the component %d times, want 1", fileSystem.reads)
		}
	})

	t.Run("a file that is not a component reads nothing", func(t *testing.T) {
		t.Parallel()
		fileSystem := newComponentFS(t, "/main.ts", "export const value = 1;\n")
		component := NewComponent(fileSystem, "/main.ts")

		if component.IsComponent() {
			t.Error("a .ts file reported itself a component")
		}
		if fileSystem.reads != 0 {
			t.Errorf("read a non-component %d times", fileSystem.reads)
		}
	})
}

func TestComponentBlocks(t *testing.T) {
	t.Parallel()

	fileSystem := newComponentFS(t, "/App.vue", componentFixture)
	component := NewComponent(fileSystem, "/App.vue")

	// Text is the component's own markup, not the blanked projection the
	// parser reads — a template rule handed the projection would see spaces.
	if got := component.Text(); got != componentFixture {
		t.Fatalf("Text() = %q, want the component's own text", got)
	}

	setup, ok := component.ScriptSetupRange()
	if !ok {
		t.Fatal("the setup block was not found")
	}
	if got := componentFixture[setup.Pos():setup.End()]; got != "\nconst local = 2;\n" {
		t.Errorf("setup range covers %q", got)
	}

	// The plain script's export sits outside the setup range, which is the
	// distinction no-export-in-script-setup is built on.
	sharedExport := indexOf(t, componentFixture, "export const shared")
	if component.InScriptSetup(sharedExport) {
		t.Error("an export in the plain block was placed inside the setup block")
	}
	localConst := indexOf(t, componentFixture, "const local")
	if !component.InScriptSetup(localConst) {
		t.Error("the setup block's own code was placed outside it")
	}

	template, ok := component.TemplateRange()
	if !ok {
		t.Fatal("the template block was not found")
	}
	if got := componentFixture[template.Pos():template.End()]; got != "\n  <div/>\n" {
		t.Errorf("template range covers %q", got)
	}
}

func TestComponentWithoutBlocks(t *testing.T) {
	t.Parallel()

	fileSystem := newComponentFS(t, "/App.vue", "<template><div/></template>\n")
	component := NewComponent(fileSystem, "/App.vue")

	if _, ok := component.ScriptSetupRange(); ok {
		t.Error("a component with no script reported a setup block")
	}
	if _, ok := component.TemplateRange(); !ok {
		t.Error("the template block was not found")
	}
	if component.InScriptSetup(0) {
		t.Error("a component with no setup block placed a position inside one")
	}
}

// TestComponentNilStore covers the shape a manually assembled rule context
// hands a rule, which must answer rather than panic.
func TestComponentNilStore(t *testing.T) {
	t.Parallel()

	var component *Component
	if component.IsComponent() {
		t.Error("a nil store reported a component")
	}
	if got := component.Text(); got != "" {
		t.Errorf("Text() = %q, want empty", got)
	}
	if component.Blocks() != nil {
		t.Error("a nil store returned blocks")
	}
	if _, ok := component.ScriptSetupRange(); ok {
		t.Error("a nil store returned a setup range")
	}
	if _, ok := component.TemplateRange(); ok {
		t.Error("a nil store returned a template range")
	}
	if component.InScriptSetup(0) {
		t.Error("a nil store placed a position inside a setup block")
	}
}

func indexOf(t *testing.T, text, needle string) int {
	t.Helper()
	for index := 0; index+len(needle) <= len(text); index++ {
		if text[index:index+len(needle)] == needle {
			return index
		}
	}
	t.Fatalf("fixture drifted: %q not found", needle)
	return -1
}
