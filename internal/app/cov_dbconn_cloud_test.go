package app

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// cdbFakeCLIs makes only the named CLIs "installed".
func cdbFakeCLIs(t *testing.T, have ...string) {
	t.Helper()
	old := dbCloudLookPath
	t.Cleanup(func() { dbCloudLookPath = old })
	dbCloudLookPath = func(name string) (string, error) {
		for _, h := range have {
			if h == name {
				return "/bin/true", nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestCovDBCloudRunContainerCardAndCancel(t *testing.T) {
	m := cdbModel(t)
	cdbFakeCLIs(t) // no docker
	m.databaseRunContainerStart()
	if !m.pickerOpen || m.pickerKind != pickerKindDBRunEngine {
		t.Fatal("no engine picker")
	}
	cdbPick(t, m, "postgres")
	if !strings.Contains(covToastText(m), "Docker is not installed") {
		t.Fatalf("toast %q", covToastText(m))
	}
	cdbFakeCLIs(t, "podman")
	if bin := m.dbContainerBinary(); bin != "/bin/true" {
		t.Fatalf("bin %q", bin)
	}
	m.databaseRunContainerStart()
	cdbPick(t, m, "redis")
	if !m.confirmOpen || m.confirmKind != confirmKindDBRunEngine || m.databases.cloud.run == nil {
		t.Fatal("no run card")
	}
	if !strings.Contains(m.databases.cloud.run.cmd.Label, "run: ") {
		t.Fatal("label")
	}
	cdbConfirm(t, m, "cancel")
	if m.databases.cloud.run != nil {
		t.Fatal("cancel kept the plan")
	}
	// an unknown engine cannot be planned
	m.databaseRunContainerCard("oracle")
	if !strings.Contains(covToastText(m), "Cannot plan the container") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// no run pending: nothing to start
	if cmd := m.databaseRunContainer(); cmd != nil {
		t.Fatal("started without a plan")
	}
	// the engine of the Services view wins over the PATH
	m.services = &servicesState{}
	m.services.containerEngine.Binary = "/usr/bin/docker-x"
	if m.dbContainerBinary() != "/usr/bin/docker-x" {
		t.Fatal("services engine")
	}
}

func TestCovDBCloudRunContainerWithoutAnEditor(t *testing.T) {
	m := cdbModel(t)
	cdbFakeCLIs(t, "docker")
	m.databaseRunContainerCard("mysql")
	cdbConfirm(t, m, "run")
	if !strings.Contains(covToastText(m), "Cannot run the container") {
		t.Fatalf("toast %q", covToastText(m))
	}
}

func TestCovDBCloudRunContainerStartsAJobAndAddsTheConnection(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("SHELL", "/bin/sh")
	m := cdbModel(t)
	c, err := nvim.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(100, 30); err != nil {
		t.Fatal(err)
	}
	_ = c.ExecLua(`vim.o.shell = '/bin/sh'`)
	m.nvim, m.terminalRows, m.output, m.tasks, m.problems = c, 8, newOutputStore(), newTaskRunner(), newProblemsPanel()
	m.activity = activity.New()
	m.services = &servicesState{}
	cdbFakeCLIs(t, "docker")
	m.databaseRunContainerCard("postgres")
	id := m.databases.cloud.run.plan.Conn.ID
	cdbConfirm(t, m, "run")
	if _, ok := m.databases.connection(id); !ok {
		t.Fatalf("connection %s not added; toast %q", id, covToastText(m))
	}
	if len(m.terminalTabs) != 1 {
		t.Fatalf("tabs %d", len(m.terminalTabs))
	}
}

func TestCovDBCloudDiscovery(t *testing.T) {
	m := cdbModel(t)
	cdbFakeCLIs(t)
	m.databaseCloudStart()
	if !strings.Contains(covToastText(m), "No cloud CLI found") {
		t.Fatalf("toast %q", covToastText(m))
	}
	cdbFakeCLIs(t, "aws", "az")
	m.databaseCloudStart()
	if !m.pickerOpen || m.pickerKind != pickerKindDBCloud {
		t.Fatal("no provider picker")
	}
	cdbPick(t, m, "aws")
	if !strings.Contains(covToastText(m), "Asking the cloud CLI") {
		t.Fatalf("toast %q", covToastText(m))
	}

	old := dbCloudCapture
	t.Cleanup(func() { dbCloudCapture = old })
	dbCloudCapture = func(_ context.Context, argv []string) ([]byte, error) {
		switch argv[0] {
		case "aws":
			return []byte(`{"DBInstances":[{"DBInstanceIdentifier":"shop-db","Engine":"postgres","Endpoint":{"Address":"shop.rds.example","Port":5432},"MasterUsername":"admin"}]}`), nil
		case "az":
			if argv[1] == "mysql" {
				return []byte("not json"), nil
			}
			return []byte(`[]`), nil
		}
		return nil, errors.New("no")
	}
	msg := databaseCloudList("aws")().(dbCloudFoundMsg)
	if msg.err != nil || len(msg.instances) != 1 {
		t.Fatalf("%+v", msg)
	}
	if msg := databaseCloudList("azure")().(dbCloudFoundMsg); msg.err == nil {
		t.Fatal("bad json accepted")
	}
	if msg := databaseCloudList("gcp")().(dbCloudFoundMsg); msg.err == nil || !strings.Contains(msg.err.Error(), "gcloud") {
		t.Fatalf("%+v", msg)
	}

	// results: error, none, some (one already added)
	m.onDBCloudFound(dbCloudFoundMsg{err: errors.New("denied")})
	if !strings.Contains(covToastText(m), "Cloud discovery failed") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.onDBCloudFound(dbCloudFoundMsg{provider: "aws"})
	if !strings.Contains(covToastText(m), "No PostgreSQL or MySQL") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.databases.conns = append(m.databases.conns, dbclient.Conn{ID: "aws-shop-db", Driver: "postgres"})
	m.onDBCloudFound(dbCloudFoundMsg{provider: "aws", instances: msg.instances})
	if !m.pickerOpen || m.pickerKind != pickerKindDBCloudPick {
		t.Fatal("no instance picker")
	}
	cdbPick(t, m, "5") // not in the list
	m.databases.cloud.found = msg.instances
	m.pickerKind = pickerKindDBCloudPick
	m.pickerOpen = true
	cdbPick(t, m, "0")
	if !m.promptOpen || m.databases.pendingConn.ID != "aws-shop-db-2" {
		t.Fatalf("pending %+v", m.databases.pendingConn)
	}
}

func TestCovDBCloudIAMLogin(t *testing.T) {
	m := cdbModel(t,
		dbclient.Conn{ID: "rds", Driver: "postgres", Host: "shop.rds.example", Port: 5432, User: "admin"},
		dbclient.Conn{ID: "ssl", Driver: "mysql", Host: "m.example", Port: 3306, User: "root", SSL: &dbclient.SSLConfig{Mode: "verify-full"}},
		dbclient.Conn{ID: "lite", Driver: "sqlite", Path: "/tmp/x.db"})
	m.databases.conns = append(m.databases.conns, dbclient.Conn{ID: "proj", Driver: "postgres", Host: "p", Source: dbclient.SourceProject})
	m.databases.nodes["proj"] = dbNode{kind: dbNodeConnection, name: "proj", path: "proj", conn: "proj"}
	for _, tc := range []struct{ id, want string }{
		{"proj", "Only your own"}, {"lite", "PostgreSQL and MySQL"},
	} {
		m.databases.databaseSelectPath(tc.id)
		m.databaseIAMStart()
		if !strings.Contains(covToastText(m), tc.want) {
			t.Fatalf("%s: toast %q", tc.id, covToastText(m))
		}
	}
	// no row / unknown connection
	m2 := cdbModel(t)
	m2.databaseIAMStart()
	if !strings.Contains(covToastText(m2), "Select a connection first") {
		t.Fatalf("toast %q", covToastText(m2))
	}
	m.databases.databaseSelectPath("rds")
	m.databaseIAMStart()
	if !m.pickerOpen || m.pickerKind != pickerKindDBIAM {
		t.Fatal("no provider picker")
	}
	cdbPick(t, m, "aws")
	if !m.confirmOpen || m.confirmKind != confirmKindDBIAM || !m.databases.cloud.iamSSLSet {
		t.Fatalf("no IAM card (ssl set %v)", m.databases.cloud.iamSSLSet)
	}
	cdbConfirm(t, m, "cancel")
	if m.databases.cloud.iamConn != "" {
		t.Fatal("cancel kept the plan")
	}
	m.databaseIAMStart()
	cdbPick(t, m, "aws")
	cdbConfirm(t, m, "use")
	c, _ := m.databases.connection("rds")
	if !strings.HasPrefix(c.Password, "${cmd:") || c.SSL == nil || c.SSL.Mode != dbclient.SSLRequire {
		t.Fatalf("not applied: %+v", c)
	}
	// SSL already set: kept
	m.databases.databaseSelectPath("ssl")
	m.databaseIAMStart()
	cdbPick(t, m, "aws")
	if !strings.Contains(m.confirm.View(), "IAM") && !m.confirmOpen {
		t.Fatal("no card")
	}
	m.confirmOpen = false
	// a provider that cannot sign this connection
	m.databases.cloud.iamConn = "lite"
	m.databaseIAMCard("aws")
	if !strings.Contains(covToastText(m), "Cannot use IAM login") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// no connection under the plan; nothing to apply
	m.databases.cloud.iamConn = "gone"
	if cmd := m.databaseIAMCard("aws"); cmd != nil {
		t.Fatal("card for a gone connection")
	}
	if cmd := m.databaseIAMApply(); cmd != nil {
		t.Fatal("apply without a plan")
	}
	m2.databases.cloud = &dbCloudState{}
	delete(m.databases.nodes, "gone")
	m.databases.cloud.iamConn, m.databases.cloud.iamRef = "gone", "${cmd:x}"
	if cmd := m.databaseIAMApply(); cmd != nil {
		t.Fatal("apply to a gone connection")
	}
	cdbBreakConfigDir(t)
	m.databases.cloud.iamConn, m.databases.cloud.iamRef = "rds", "${cmd:x}"
	m.databaseIAMApply()
	if !strings.Contains(covToastText(m), "Cannot save the connection") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// router: unknown kinds fall through
	m.pickerKind = 0
	if _, ok := m.databaseCloudPicker("x"); ok {
		t.Fatal("foreign picker")
	}
	m.confirmKind = 0
	if _, ok := m.databaseCloudConfirm("x"); ok {
		t.Fatal("foreign card")
	}
}

func TestCovDBCloudSmallHelpers(t *testing.T) {
	if len(dbRandHex(4)) != 8 {
		t.Fatal("hex length")
	}
	p, err := dbFreePort()
	if err != nil || p < 1 {
		t.Fatalf("port %d %v", p, err)
	}
}

func TestCovDBCloudDefaultsRunRealCLIsThroughTheGuard(t *testing.T) {
	ctx := context.Background()
	if _, err := dbCloudCapture(ctx, []string{"true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := dbCloudCapture(ctx, []string{"false"}); err == nil {
		t.Fatal("a failing CLI reads as success")
	}
}

// A connection with a kube block opens through a port-forward; a Redis
// session ends the forward when it closes, others through OnClose.
func TestCovDBCloudKubeOpenEndsTheForwardOnClose(t *testing.T) {
	stopped := 0
	old := dbKubeForward
	t.Cleanup(func() { dbKubeForward = old })
	dbKubeForward = func(_ context.Context, c dbclient.Conn) (int, func(), error) {
		return 41234, func() { stopped++ }, nil
	}
	kube := &dbclient.KubeConfig{Pod: "redis-0", Port: 6379}
	f := cdbRedisSeed()
	oldDial := dbRedisDial
	dbRedisDial = f.Dial
	t.Cleanup(func() { dbRedisDial = oldDial; f.Close() })
	c := dbclient.Conn{ID: "k", Driver: "redis", Kube: kube}
	sess, err := dbKubeOpen(context.Background(), c, func(c dbclient.Conn) (dbclient.Session, error) {
		if c.Host != "127.0.0.1" || c.Port != 41234 {
			t.Errorf("not forwarded: %+v", c)
		}
		return dbRedisSessionFactory(context.Background(), c, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sess.(dbStopRedis); !ok {
		t.Fatalf("a %T", sess)
	}
	if stopped != 0 {
		t.Fatal("stopped early")
	}
	_ = sess.Close()
	if stopped != 1 {
		t.Fatalf("stopped %d", stopped)
	}
	// a plain SQL session, a sqlite file, a refused forward, a failed open
	pg := dbclient.Conn{ID: "p", Driver: "postgres", Kube: kube}
	sess, err = dbKubeOpen(context.Background(), pg, func(dbclient.Conn) (dbclient.Session, error) { return &cdbSession{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	_ = sess.Close()
	if stopped != 2 {
		t.Fatalf("stopped %d", stopped)
	}
	if _, err := dbKubeOpen(context.Background(), dbclient.Conn{Driver: "sqlite", Kube: kube}, func(dbclient.Conn) (dbclient.Session, error) { return &cdbSession{}, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := dbKubeOpen(context.Background(), pg, func(dbclient.Conn) (dbclient.Session, error) { return nil, errors.New("refused") }); err == nil || stopped != 3 {
		t.Fatalf("failed open: %v stopped %d", err, stopped)
	}
	dbKubeForward = func(context.Context, dbclient.Conn) (int, func(), error) { return 0, nil, errors.New("no kubectl") }
	if _, err := dbKubeOpen(context.Background(), pg, nil); err == nil || !strings.Contains(err.Error(), "port-forward to pod") {
		t.Fatalf("%v", err)
	}
}
