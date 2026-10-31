package secrets

import "testing"

func TestMigrateKeepsPlainCopyByDefault(t *testing.T) {
	dir, path := migrateDir(t, migrateFixture)
	s, err := Migrate(MigrateOptions{Options: Options{Dir: dir, PlainPath: path, ScryptWorkFactor: 10}, To: "age", Pass: []byte(pass)})
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != migrateFixture {
		t.Fatal("migration changed the plain copy without consent")
	}
	checkValues(t, s)
	if done, err := Finish(s, path); !done || err != nil {
		t.Fatalf("explicit deletion = %v, %v", done, err)
	}
	gone(t, path)
}

func TestSwitchKeepsPlainCopyByDefault(t *testing.T) {
	dir, path := migrateDir(t, migrateFixture)
	opts := Options{Dir: dir, PlainPath: path, Backend: "plain", ScryptWorkFactor: 10}
	from, err := Open(opts)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Switch(from, SwitchOptions{Options: opts, To: "age", Pass: []byte(pass)})
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != migrateFixture {
		t.Fatal("switch changed the plain copy without consent")
	}
	checkValues(t, s)
}
