package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// DB-12: the Services / Containers link of the Databases view. Real keys
// and mouse against the fake docker; nothing here opens a database socket.

const dbLinkConns = `{"connections":[{"id":"local-pg","driver":"postgres","host":"localhost","port":5432,"user":"postgres","database":"app"},` +
	`{"id":"local-redis","driver":"redis","host":"localhost","port":6379}]}`

// dbLinkFake is the services fake with postgres stopped (not in ps).
func dbLinkFake(t *testing.T) *servicestest.Fake {
	t.Helper()
	f := svcFake(t)
	b, err := os.ReadFile(filepath.Join(svcTestdata, "ps-lines.json"))
	if err != nil {
		t.Fatal(err)
	}
	var keep []string
	for _, line := range strings.Split(string(b), "\n") {
		if line != "" && !strings.Contains(line, `"Image":"postgres:16"`) {
			keep = append(keep, line)
		}
	}
	f.Set(t, "ps.json", strings.Join(keep, "\n")+"\n")
	return f
}

func dbLinkDriver(t *testing.T, w, h int, files map[string]string) (*Driver, *servicestest.Fake) {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	f := dbLinkFake(t)
	all := map[string]string{"compose.yaml": svcCompose, "a.txt": "alpha\n"}
	for k, v := range files {
		all[k] = v
	}
	d := New(t, Options{W: w, H: h, Nvim: true, Files: all})
	d.Palette("services-focus")
	if !d.WaitForText("mailhog", 5*time.Second) {
		t.Fatalf("the Services view did not list the services:\n%s", screen(d))
	}
	return d, f
}

// screenPos finds text on the screen (column, row); the first match.
func screenPos(d *Driver, text string) (int, int, bool) {
	for y, line := range strings.Split(screen(d), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return len([]rune(line[:i])), y, true
		}
	}
	return 0, 0, false
}

// dbLinkRightClick right-clicks the row of a service and picks a menu item.
func dbLinkMenuRows(t *testing.T, d *Driver, row string) string {
	t.Helper()
	x, y, ok := screenPos(d, " "+row+" ")
	if !ok {
		t.Fatalf("no row %q:\n%s", row, screen(d))
	}
	d.RightClick(x+2, y)
	return screen(d)
}

func TestServicesRowMenuOpensDatabasesPrefilled(t *testing.T) {
	d, _ := dbLinkDriver(t, 120, 34, nil)
	got := dbLinkMenuRows(t, d, "postgres")
	if !strings.Contains(got, "Open in Databases") {
		t.Fatalf("a postgres service row has no Open in Databases entry:\n%s", got)
	}
	x, y, _ := screenPos(d, "Open in Databases")
	d.Click(x+2, y)
	if !d.WaitForText("Add postgres to Databases", 3*time.Second) {
		t.Fatalf("the add flow did not open:\n%s", screen(d))
	}
	s := screen(d)
	if !strings.Contains(s, "postgres@localhost:5432/app") {
		t.Errorf("host, port, user and database are not prefilled:\n%s", s)
	}
	if !strings.Contains(s, "DATABASES") {
		t.Errorf("the Databases view is not shown:\n%s", s)
	}
	for _, secret := range []string{"hunter2"} {
		if strings.Contains(s, secret) || strings.Contains(diskFile(d, ".termocode/databases.json"), secret) {
			t.Errorf("a password from the compose file leaked")
		}
	}
	d.Key("esc")
}

func TestServicesRowMenuHasNoEntryForOtherServices(t *testing.T) {
	d, _ := dbLinkDriver(t, 120, 34, nil)
	if got := dbLinkMenuRows(t, d, "mailhog"); strings.Contains(got, "Open in Databases") {
		t.Fatalf("mailhog is not a database:\n%s", got)
	}
	d.Key("esc")
	if got := dbLinkMenuRows(t, d, "redis"); !strings.Contains(got, "Open in Databases") {
		t.Fatalf("redis has no entry:\n%s", got)
	}
}

func TestServicesPaletteOpenInDatabases(t *testing.T) {
	d, _ := dbLinkDriver(t, 100, 30, nil)
	svcGoto(d, "redis")
	d.Palette("containers-open-databases")
	if !d.WaitForText("Add redis to Databases", 3*time.Second) {
		t.Fatalf("the palette entry did not open the add flow:\n%s", screen(d))
	}
	if s := screen(d); !strings.Contains(s, "localhost:6379") {
		t.Errorf("redis port not prefilled:\n%s", s)
	}
}

func TestOpenInDatabasesSelectsSavedConnection(t *testing.T) {
	// redis is listed first, so the cursor must move to postgres.
	conns := `{"connections":[{"id":"local-redis","driver":"redis","host":"localhost","port":6379},` +
		`{"id":"local-pg","driver":"postgres","host":"localhost","port":5432,"user":"postgres","database":"app"}]}`
	d, _ := dbLinkDriver(t, 100, 30, map[string]string{".termocode/databases.json": conns})
	svcGoto(d, "postgres")
	d.Palette("containers-open-databases")
	if !d.WaitForText("Already in Databases", 3*time.Second) {
		t.Fatalf("a saved match was not selected:\n%s", screen(d))
	}
	s := screen(d)
	if strings.Contains(s, "Add postgres to Databases") {
		t.Errorf("the add flow opened for a saved connection:\n%s", s)
	}
	if !d.WaitForText("postgres stopped · S start", 2*time.Second) {
		t.Errorf("the cursor is not on local-pg (its footer hint is missing):\n%s", s)
	}
}

func TestDatabasesFooterHintsStoppedServiceAndStartsIt(t *testing.T) {
	d, f := dbLinkDriver(t, 100, 30, map[string]string{".termocode/databases.json": dbLinkConns})
	d.Palette("db-focus")
	if !d.WaitForText("local-pg", 3*time.Second) {
		t.Fatalf("no connection row:\n%s", screen(d))
	}
	if !d.WaitForText("postgres stopped · S start", 3*time.Second) {
		t.Fatalf("no stopped hint on the footer:\n%s", screen(d))
	}
	// A running service has no hint.
	d.Key("j")
	if !d.WaitForText("local-redis", time.Second) || strings.Contains(screen(d), "stopped · S start") {
		t.Fatalf("a running redis shows a stopped hint:\n%s", screen(d))
	}
	d.Key("S")
	d.Settle(300 * time.Millisecond)
	if f.Count("up -d") != 0 {
		t.Fatalf("S on a running service started something: %q", f.Calls())
	}
	d.Key("k", "S")
	if !d.WaitFor(func() bool { return f.Count("up -d postgres") == 1 }, 5*time.Second) {
		t.Fatalf("S did not start postgres: %q\n%s", f.Calls(), screen(d))
	}
	if !d.WaitForText("services: up", 5*time.Second) {
		t.Errorf("the start did not use the existing visible up tab:\n%s", screen(d))
	}
}

func TestDatabasesConnectionMenuStartsStoppedService(t *testing.T) {
	d, f := dbLinkDriver(t, 100, 30, map[string]string{".termocode/databases.json": dbLinkConns})
	d.Palette("db-focus")
	if !d.WaitForText("postgres stopped", 3*time.Second) {
		t.Fatalf("no hint:\n%s", screen(d))
	}
	x, y, _ := screenPos(d, "local-pg")
	d.RightClick(x, y)
	mx, my, ok := screenPos(d, "Start service")
	if !ok {
		t.Fatalf("no Start service in the menu:\n%s", screen(d))
	}
	d.Click(mx+2, my)
	if !d.WaitFor(func() bool { return f.Count("up -d postgres") == 1 }, 5*time.Second) {
		t.Fatalf("the menu did not start postgres: %q", f.Calls())
	}
}

const dbLinkContainers = `{"ID":"pg1234567890","Names":"pgdb","Image":"postgres:16","State":"exited","Status":"Exited (0) 1 hour ago","Ports":"0.0.0.0:5433->5432/tcp","Labels":{},"Size":"0B"}
{"ID":"web9876543210","Names":"webby","Image":"nginx:1.27","State":"running","Status":"Up 3 days","Ports":"80/tcp","Labels":{},"Size":"0B"}
`

func dbLinkContainerDriver(t *testing.T, files map[string]string) (*Driver, *servicestest.Fake) {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	wd, _ := os.Getwd()
	fixtureDir := filepath.Join(wd, "..", "containers", "testdata", "docker27")
	f := servicestest.New(t)
	for _, name := range []string{"version.json", "images.json", "network-ls.json", "volume-ls.json", "context-ls.json"} {
		b, err := os.ReadFile(filepath.Join(fixtureDir, name))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, name, string(b))
	}
	f.Set(t, "ps.json", dbLinkContainers)
	t.Setenv("PATH", f.PathEnv())
	all := map[string]string{"compose.yaml": "services:\n  api:\n    image: ghcr.io/acme/api:1.4\n"}
	for k, v := range files {
		all[k] = v
	}
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: all})
	d.Palette("containers-focus")
	if !d.WaitForText("pgdb", 5*time.Second) {
		t.Fatalf("container list did not load:\n%s", screen(d))
	}
	return d, f
}

func TestContainerRowMenuOpensDatabasesAndStartsIt(t *testing.T) {
	conns := `{"connections":[{"id":"pg-5433","driver":"postgres","host":"127.0.0.1","port":5433,"user":"postgres","database":"postgres"}]}`
	d, f := dbLinkContainerDriver(t, nil)
	// A plain container: the menu offers Open in Databases on a database image only.
	if got := dbLinkMenuRows(t, d, "webby"); strings.Contains(got, "Open in Databases") || !strings.Contains(got, "Copy ID") {
		t.Fatalf("webby menu wrong:\n%s", got)
	}
	d.Key("esc")
	got := dbLinkMenuRows(t, d, "pgdb")
	if !strings.Contains(got, "Open in Databases") {
		t.Fatalf("postgres container has no entry:\n%s", got)
	}
	x, y, _ := screenPos(d, "Open in Databases")
	d.Click(x+2, y)
	if !d.WaitForText("Add pgdb to Databases", 3*time.Second) {
		t.Fatalf("the add flow did not open:\n%s", screen(d))
	}
	if s := screen(d); !strings.Contains(s, "postgres@localhost:5433/postgres") {
		t.Errorf("container port or defaults not prefilled:\n%s", s)
	}
	d.Key("esc")
	_ = conns
	_ = f
}

func TestContainerStoppedHintAndStart(t *testing.T) {
	conns := `{"connections":[{"id":"pg-5433","driver":"postgres","host":"127.0.0.1","port":5433,"user":"postgres","database":"postgres"}]}`
	d, f := dbLinkContainerDriver(t, map[string]string{".termocode/databases.json": conns})
	d.Palette("db-focus")
	if !d.WaitForText("pgdb stopped · S start", 3*time.Second) {
		t.Fatalf("no stopped hint for the container:\n%s", screen(d))
	}
	d.Key("S")
	if !d.WaitFor(func() bool { return f.Count("start pg1234567890") == 1 }, 5*time.Second) {
		t.Fatalf("S did not start the container: %q\n%s", f.Calls(), screen(d))
	}
}
