package testkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// DB-16 / DB-17: the Databases view finds servers itself, and the mouse
// reaches every action. Real mouse events; the container list comes from
// the fake docker, nothing talks to a real engine.

// dbDiscoverOn must run after New: the sandbox switches discovery off.
func dbDiscoverOn(t *testing.T) { t.Setenv("TERMOCODE_NO_DB_DISCOVERY", "") }

func dbDriver(t *testing.T, files map[string]string, setup func(testing.TB, string)) *Driver {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	all := map[string]string{"a.txt": "alpha\n"}
	for k, v := range files {
		all[k] = v
	}
	return New(t, Options{W: 110, H: 30, Nvim: true, Files: all, Setup: setup})
}

// dbDouble clicks the first row that shows text, twice and fast.
func dbDouble(t *testing.T, d *Driver, text string) {
	t.Helper()
	x, y, ok := screenPos(d, text)
	if !ok {
		t.Fatalf("no %q on the screen:\n%s", text, screen(d))
	}
	d.Click(x+1, y)
	d.Click(x+1, y)
}

func dbSingle(t *testing.T, d *Driver, text string) {
	t.Helper()
	x, y, ok := screenPos(d, text)
	if !ok {
		t.Fatalf("no %q on the screen:\n%s", text, screen(d))
	}
	d.Click(x+1, y)
}

// dbContainerFake is the fake docker with one PostgreSQL container.
func dbContainerFake(t *testing.T) {
	t.Helper()
	wd, _ := os.Getwd()
	dir := filepath.Join(wd, "..", "containers", "testdata", "docker27")
	f := servicestest.New(t)
	for _, name := range []string{"version.json", "images.json", "network-ls.json", "volume-ls.json", "context-ls.json"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, name, string(b))
	}
	f.Set(t, "ps.json", `{"ID":"pg1234","Names":"shop-pg","Image":"postgres:16","State":"running","Status":"Up 2 hours","Ports":"0.0.0.0:55432->5432/tcp","Labels":"","Size":"1MB"}`+"\n")
	t.Setenv("PATH", f.PathEnv())
}

func TestDatabasesFindContainersWithoutServicesView(t *testing.T) {
	dbContainerFake(t)
	d := dbDriver(t, nil, nil)
	dbDiscoverOn(t) // after New: the sandbox switches it off
	d.Palette("db-focus")
	if !d.WaitForText("shop-pg (container)", 5*time.Second) {
		t.Fatalf("the Databases view did not list the containers by itself:\n%s", screen(d))
	}
}

func TestDatabasesEmptyStateButtonsAreClickable(t *testing.T) {
	d := dbDriver(t, nil, nil)
	d.Palette("db-focus")
	if !d.WaitForText("Add connection (a)", 3*time.Second) || !strings.Contains(screen(d), "Add remote") {
		t.Fatalf("empty state lacks its actions:\n%s", screen(d))
	}
	dbSingle(t, d, "Add connection (a)")
	if !d.WaitForText("PostgreSQL", 3*time.Second) {
		t.Fatalf("a click on 'Add connection (a)' did not open the type picker:\n%s", screen(d))
	}
	d.Key("esc")
	dbSingle(t, d, "Add remote")
	if !d.WaitForText("remote database", 3*time.Second) {
		t.Fatalf("a click on 'Add remote' did not open the remote picker:\n%s", screen(d))
	}
}

func TestDatabasesHeaderRefreshClickFindsNewFile(t *testing.T) {
	d := dbDriver(t, map[string]string{"one.db": "x"}, nil)
	d.Palette("db-focus")
	if !d.WaitForText("one.db", 3*time.Second) {
		t.Fatalf("no suggestion:\n%s", screen(d))
	}
	if err := os.WriteFile(filepath.Join(d.Dir, "two.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dbSingle(t, d, "R refresh")
	if !d.WaitForText("two.db", 3*time.Second) {
		t.Fatalf("a click on 'R refresh' did not read the workspace again:\n%s", screen(d))
	}
	dbSingle(t, d, "a add")
	if !d.WaitForText("PostgreSQL", 3*time.Second) {
		t.Fatalf("a click on 'a add' did not open the type picker:\n%s", screen(d))
	}
}

func TestDatabasesSuggestionSingleClickSelectsDoubleClickAdds(t *testing.T) {
	d := dbDriver(t, map[string]string{"one.db": "x"}, nil)
	d.Palette("db-focus")
	if !d.WaitForText("one.db", 3*time.Second) {
		t.Fatalf("no suggestion:\n%s", screen(d))
	}
	dbSingle(t, d, "one.db")
	if !d.WaitForText("a add suggestion", 2*time.Second) || !strings.Contains(screen(d), "SUGGESTED") {
		t.Fatalf("one click should only select the suggestion:\n%s", screen(d))
	}
	time.Sleep(500 * time.Millisecond) // not a double click
	dbDouble(t, d, "one.db")
	if !d.WaitFor(func() bool { return !strings.Contains(screen(d), "SUGGESTED") }, 3*time.Second) {
		t.Fatalf("a double click did not add the suggestion:\n%s", screen(d))
	}
	b, _ := os.ReadFile(filepath.Join(d.Dir, ".termocode", "databases.json"))
	if !strings.Contains(string(b), `"one.db"`) || strings.Contains(strings.ToLower(string(b)), "password") {
		t.Fatalf("saved file: %s", b)
	}
}

func TestDatabasesDoubleClickConnectFoldAndOpenData(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	d := dbDriver(t, map[string]string{".termocode/databases.json": `{"connections":[{"id":"app","name":"app","driver":"sqlite","path":"app.db"}]}`},
		func(tb testing.TB, dir string) {
			out, err := exec.Command("sqlite3", filepath.Join(dir, "app.db"),
				"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT); INSERT INTO users (email) VALUES ('ada@example.com');").CombinedOutput()
			if err != nil {
				tb.Skipf("sqlite3: %v %s", err, out)
			}
		})
	d.Palette("db-focus")
	if !d.WaitForText("app", 3*time.Second) {
		t.Fatalf("no connection row:\n%s", screen(d))
	}
	dbDouble(t, d, "app  sqlite")
	if !d.WaitForText("main", 10*time.Second) {
		t.Fatalf("a double click did not connect:\n%s", screen(d))
	}
	dbDouble(t, d, "main")
	if !d.WaitForText("users", 10*time.Second) {
		t.Fatalf("a double click did not expand the database:\n%s", screen(d))
	}
	// A folder folds on a double click and unfolds on the next.
	for _, group := range []string{"Tables", "tables"} {
		if _, _, ok := screenPos(d, group); ok {
			dbDouble(t, d, group)
			if !d.WaitFor(func() bool { return !strings.Contains(screen(d), "users") }, 3*time.Second) {
				t.Fatalf("a double click did not fold %s:\n%s", group, screen(d))
			}
			time.Sleep(500 * time.Millisecond)
			dbDouble(t, d, group)
			if !d.WaitForText("users", 3*time.Second) {
				t.Fatalf("a double click did not unfold %s:\n%s", group, screen(d))
			}
			break
		}
	}
	dbDouble(t, d, "users")
	if !d.WaitForText("ada@example.com", 10*time.Second) {
		t.Fatalf("a double click on a table did not open its data:\n%s", screen(d))
	}
}
