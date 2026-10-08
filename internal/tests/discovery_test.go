package tests

import (
	"os"
	"path/filepath"
	"testing"
)

func names(cs []*Case) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

// TS-02: TestXxx / FuzzXxx discovered with 1-based lines; helpers,
// benchmarks, TestMain and lower-case suffixes ignored.
func TestParseGoTests(t *testing.T) {
	src := `package foo

import "testing"

func TestMain(m *testing.M) {}

func TestAlpha(t *testing.T) {}

func setup() {}

func BenchmarkBeta(b *testing.B) {}

func Testlower(t *testing.T) {}

func Test(t *testing.T) {}

func FuzzParse(f *testing.F) {}

func Test_under(t *testing.T) {}
`
	cs := ParseGoTests("/p/foo_test.go", src)
	eq(t, names(cs), []string{"TestAlpha", "Test", "FuzzParse", "Test_under"})
	if cs[0].Line != 7 || cs[0].File != "/p/foo_test.go" {
		t.Fatalf("TestAlpha at %s:%d, want line 7", cs[0].File, cs[0].Line)
	}
}

func TestParsePytest(t *testing.T) {
	src := `import pytest

def helper():
    pass

def test_add():
    assert 1

async def test_async():
    pass

class TestOps:
    def test_mul(self):
        pass

    def helper(self):
        pass

class Other:
    def test_not_collected(self):
        pass

def test_after_class():
    pass
`
	cs := ParsePytest("/p/test_x.py", src)
	eq(t, names(cs), []string{"test_add", "test_async", "TestOps::test_mul", "test_after_class"})
	if cs[2].Line != 13 {
		t.Fatalf("TestOps::test_mul line = %d, want 13", cs[2].Line)
	}
}

func TestParseRustTests(t *testing.T) {
	src := `pub fn add(a: i32, b: i32) -> i32 { a + b }

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn it_adds() {
        assert_eq!(add(1, 2), 3);
    }

    #[test]
    #[ignore]
    fn slow_one() {}

    fn helper() {}

    #[tokio::test]
    async fn it_awaits() {}
}

#[test]
fn top_level() {}
`
	cs := ParseRustTests("/p/src/lib.rs", src)
	eq(t, names(cs), []string{"tests::it_adds", "tests::slow_one", "tests::it_awaits", "top_level"})
	if cs[0].Line != 8 {
		t.Fatalf("it_adds line = %d, want 8", cs[0].Line)
	}
}

func TestParseJSTests(t *testing.T) {
	src := "describe('math', () => {\n" +
		"  it('adds', () => {})\n" +
		"  describe(\"nested\", () => {\n" +
		"    test.only(`subtracts`, () => {})\n" +
		"  })\n" +
		"  it.skip('later', () => {})\n" +
		"})\n" +
		"test('top level', () => {})\n"
	cs := ParseJSTests("/p/a.test.js", src)
	eq(t, names(cs), []string{"math › adds", "math › nested › subtracts", "math › later", "top level"})
	if cs[1].Line != 4 {
		t.Fatalf("subtracts line = %d, want 4", cs[1].Line)
	}
}

func TestIsTestFile(t *testing.T) {
	cases := []struct {
		fw   Framework
		name string
		want bool
	}{
		{FrameworkGo, "a/foo_test.go", true},
		{FrameworkGo, "a/foo.go", false},
		{FrameworkPytest, "tests/test_a.py", true},
		{FrameworkPytest, "tests/a_test.py", true},
		{FrameworkPytest, "tests/conftest.py", false},
		{FrameworkCargo, "src/lib.rs", true},
		{FrameworkJest, "src/a.test.ts", true},
		{FrameworkJest, "src/a.spec.jsx", true},
		{FrameworkJest, "src/__tests__/a.js", true},
		{FrameworkJest, "src/a.js", false},
	}
	for _, c := range cases {
		if got := IsTestFile(c.fw, c.name); got != c.want {
			t.Errorf("IsTestFile(%v, %q) = %v, want %v", c.fw, c.name, got, c.want)
		}
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TS-01/TS-02: discovery walks the tree, groups by package and skips
// vendor / hidden / testdata dirs and files without tests.
func TestDetectAndDiscoverGo(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/demo\n\ngo 1.22\n")
	write(t, root, "root_test.go", "package demo\nfunc TestRoot(t *testing.T) {}\n")
	write(t, root, "pkg/foo_test.go", "package pkg\nfunc TestAlpha(t *testing.T) {}\nfunc TestBeta(t *testing.T) {}\n")
	write(t, root, "pkg/helper_test.go", "package pkg\nfunc helper() {}\n")
	write(t, root, "vendor/x/x_test.go", "package x\nfunc TestVendored(t *testing.T) {}\n")
	write(t, root, ".git/y_test.go", "package y\nfunc TestHidden(t *testing.T) {}\n")
	write(t, root, "pkg/testdata/z_test.go", "package z\nfunc TestData(t *testing.T) {}\n")

	fw := Detect(root)
	if fw != FrameworkGo {
		t.Fatalf("Detect = %v, want go", fw)
	}
	pkgs, err := Discover(root, fw)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range pkgs {
		got = append(got, p.Name)
	}
	eq(t, got, []string{"example.com/demo", "example.com/demo/pkg"})
	if n := len(pkgs[1].Files); n != 1 {
		t.Fatalf("pkg files = %d, want 1 (helper-only file has no suite)", n)
	}
	if c := CountAll(pkgs); c.Total != 3 {
		t.Fatalf("total = %d, want 3", c.Total)
	}
}

func TestDetectFrameworks(t *testing.T) {
	cases := []struct {
		file, body string
		want       Framework
	}{
		{"Cargo.toml", "[package]\n", FrameworkCargo},
		{"pyproject.toml", "[tool.pytest]\n", FrameworkPytest},
		{"package.json", `{"devDependencies":{"jest":"^29"}}`, FrameworkJest},
		{"package.json", `{"devDependencies":{"vitest":"^1"}}`, FrameworkVitest},
	}
	for _, c := range cases {
		root := t.TempDir()
		write(t, root, c.file, c.body)
		if got := Detect(root); got != c.want {
			t.Errorf("%s: Detect = %v, want %v", c.file, got, c.want)
		}
	}
	if got := Detect(t.TempDir()); got != FrameworkNone {
		t.Errorf("empty dir: Detect = %v, want none", got)
	}
}

func TestCarryOverKeepsResults(t *testing.T) {
	old := []*Package{{Files: []*File{{Cases: []*Case{{Name: "A", File: "/f", Status: StatusFailed, Message: "boom"}, {Name: "B", File: "/f", Status: StatusRunning}}}}}}
	fresh := []*Package{{Files: []*File{{Cases: []*Case{{Name: "A", File: "/f"}, {Name: "B", File: "/f"}, {Name: "C", File: "/f"}}}}}}
	CarryOver(old, fresh)
	cs := fresh[0].Files[0].Cases
	if cs[0].Status != StatusFailed || cs[0].Message != "boom" {
		t.Fatalf("A = %+v, want failed/boom", cs[0])
	}
	if cs[1].Status != StatusPending || cs[2].Status != StatusPending {
		t.Fatalf("B/C should be pending, got %v / %v", cs[1].Status, cs[2].Status)
	}
}
