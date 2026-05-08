package shared

import (
	"strings"
	"testing"
)

// validInventreeSpec returns the canonical example used across most tests.
// Hand-written so a reader can immediately see what a real substrate looks
// like; mirror what bao-app-enroll will produce.
func validInventreeSpec() *Schmutz {
	return &Schmutz{
		Version:      SchmutzSchemaVersion,
		Tenant:       "kontango",
		App:          "inventree",
		Deployment:   "prod-01",
		AppRef:       "inventree",
		EntityID:     "1631ac1c-7a79-017c-047a-b98cd827c85d",
		ZitiIdentity: "machine-f6f769f1",
		Binds: []Bind{
			{Service: "inventree.tango", LocalAddr: "127.0.0.1:8000", Proto: "tcp"},
			{Service: "ssh-inventree.tango", LocalAddr: "127.0.0.1:22", Proto: "tcp"},
		},
		Routes: []Route{
			{Host: "inventree.tango", Upstream: "http://127.0.0.1:8000", TLS: true},
		},
		Posture: &Posture{
			MinOS:            "linux",
			RequireProcesses: []string{"/usr/local/bin/ziti-edge-tunnel"},
			RequireFiles: []FileCheck{
				{Path: "/etc/schmutz/agent.json"},
			},
		},
	}
}

func TestValidate_Happy(t *testing.T) {
	if err := validInventreeSpec().Validate(); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestValidate_VersionMismatch(t *testing.T) {
	s := validInventreeSpec()
	s.Version = 99
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "unsupported version 99") {
		t.Errorf("expected version error, got %v", err)
	}
}

func TestValidate_BadSlugs(t *testing.T) {
	cases := []struct {
		mut  func(*Schmutz)
		want string
	}{
		{func(s *Schmutz) { s.Tenant = "Kontango" }, "tenant"},
		{func(s *Schmutz) { s.Tenant = "" }, "tenant"},
		{func(s *Schmutz) { s.App = "Inventree" }, "app"},
		{func(s *Schmutz) { s.App = "inv_entree" }, "app"}, // underscore not allowed
		{func(s *Schmutz) { s.Deployment = "-prod-01" }, "deployment"},
		{func(s *Schmutz) { s.Deployment = "prod-01-" }, "deployment"},
	}
	for _, c := range cases {
		s := validInventreeSpec()
		c.mut(s)
		err := s.Validate()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("mut: expected %q in error, got %v", c.want, err)
		}
	}
}

func TestValidate_BadZitiIdentity(t *testing.T) {
	cases := []string{
		"",                  // empty
		"vm-12345678",       // wrong prefix
		"machine-1",         // too short
		"machine-zzzzzzzz",  // non-hex
		"Machine-12345678",  // uppercase prefix
	}
	for _, id := range cases {
		s := validInventreeSpec()
		s.ZitiIdentity = id
		err := s.Validate()
		if err == nil || !strings.Contains(err.Error(), "ziti_identity") {
			t.Errorf("id=%q: expected ziti_identity error, got %v", id, err)
		}
	}
}

func TestValidate_RequiresAtLeastOneBind(t *testing.T) {
	s := validInventreeSpec()
	s.Binds = nil
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "at least one bind") {
		t.Errorf("expected 'at least one bind', got %v", err)
	}
}

func TestValidate_BindShape(t *testing.T) {
	cases := []struct {
		mut  func(*Bind)
		want string
	}{
		{func(b *Bind) { b.Service = "inventree.local" }, ".tango"},
		{func(b *Bind) { b.Service = "Inventree.tango" }, ".tango"},
		{func(b *Bind) { b.LocalAddr = "" }, "local_addr"},
		{func(b *Bind) { b.LocalAddr = "127.0.0.1" }, "missing port"},
		{func(b *Bind) { b.LocalAddr = ":8000" }, "missing host"},
		{func(b *Bind) { b.Proto = "sctp" }, "tcp or udp"},
	}
	for _, c := range cases {
		s := validInventreeSpec()
		c.mut(&s.Binds[0])
		err := s.Validate()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("mut: expected %q, got %v", c.want, err)
		}
	}
}

func TestValidate_RouteRequiresMatchingBind(t *testing.T) {
	s := validInventreeSpec()
	s.Routes = []Route{
		{Host: "ghost.tango", Upstream: "http://127.0.0.1:8000"},
	}
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "no matching bind") {
		t.Errorf("expected matching-bind error, got %v", err)
	}
}

func TestValidate_RouteShape(t *testing.T) {
	cases := []struct {
		mut  func(*Route)
		want string
	}{
		{func(r *Route) { r.Host = "inventree.local" }, ".tango"},
		{func(r *Route) { r.Upstream = "" }, "upstream required"},
		{func(r *Route) { r.Upstream = "127.0.0.1:8000" }, "http:// or https://"},
		{func(r *Route) { r.Upstream = "tcp://127.0.0.1:8000" }, "http:// or https://"},
	}
	for _, c := range cases {
		s := validInventreeSpec()
		c.mut(&s.Routes[0])
		err := s.Validate()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("mut: expected %q, got %v", c.want, err)
		}
	}
}

func TestValidate_DuplicateBindServices(t *testing.T) {
	s := validInventreeSpec()
	s.Binds = []Bind{
		{Service: "inventree.tango", LocalAddr: "127.0.0.1:8000"},
		{Service: "inventree.tango", LocalAddr: "127.0.0.1:8001"},
	}
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate service") {
		t.Errorf("expected duplicate-service error, got %v", err)
	}
}

func TestValidate_PostureFiles(t *testing.T) {
	cases := []struct {
		mut  func(*Posture)
		want string
	}{
		{func(p *Posture) { p.RequireFiles = []FileCheck{{Path: ""}} }, "path required"},
		{func(p *Posture) { p.RequireFiles = []FileCheck{{Path: "etc/relative"}} }, "absolute"},
		{func(p *Posture) { p.RequireFiles = []FileCheck{{Path: "/etc/x", Sha512: "abc"}} }, "128 hex"},
		{func(p *Posture) { p.RequireProcesses = []string{"ziti-edge-tunnel"} }, "absolute path"},
	}
	for _, c := range cases {
		s := validInventreeSpec()
		c.mut(s.Posture)
		err := s.Validate()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("mut: expected %q, got %v", c.want, err)
		}
	}
}

func TestMatchesPath_Happy(t *testing.T) {
	s := validInventreeSpec()
	if err := s.MatchesPath("kontango", "inventree", "prod-01"); err != nil {
		t.Errorf("expected match, got %v", err)
	}
}

func TestMatchesPath_Mismatch(t *testing.T) {
	cases := []struct {
		t, a, d, want string
	}{
		{"acme", "inventree", "prod-01", "tenant"},
		{"kontango", "ticketarr", "prod-01", "app"},
		{"kontango", "inventree", "dev-01", "deployment"},
	}
	for _, c := range cases {
		s := validInventreeSpec()
		err := s.MatchesPath(c.t, c.a, c.d)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("(%s,%s,%s): expected %q, got %v", c.t, c.a, c.d, c.want, err)
		}
	}
}

// EffectiveBinds: base SSH bind always prepended unless IncludeBase=false.
func TestEffectiveBinds_PrependSSH(t *testing.T) {
	s := validInventreeSpec()
	s.Deployment = "prod-01"
	s.Binds = []Bind{
		{Service: "inventree.tango", LocalAddr: "127.0.0.1:8000", Proto: "tcp"},
	}
	binds := s.EffectiveBinds("")
	if len(binds) != 2 {
		t.Fatalf("expected 2 binds (SSH + inventree), got %d", len(binds))
	}
	if binds[0].Service != "ssh-prod-01.tango" {
		t.Errorf("first bind should be SSH, got %q", binds[0].Service)
	}
	if binds[1].Service != "inventree.tango" {
		t.Errorf("second bind should be inventree, got %q", binds[1].Service)
	}
}

func TestEffectiveBinds_NoBaseWhenExplicitFalse(t *testing.T) {
	f := false
	s := validInventreeSpec()
	s.IncludeBase = &f
	s.Binds = []Bind{{Service: "inventree.tango", LocalAddr: "127.0.0.1:8000"}}
	binds := s.EffectiveBinds("")
	if len(binds) != 1 || binds[0].Service != "inventree.tango" {
		t.Errorf("expected only declared bind when IncludeBase=false, got %v", binds)
	}
}

func TestEffectiveBinds_NoSSHDuplication(t *testing.T) {
	s := validInventreeSpec()
	s.Deployment = "prod-01"
	// Operator explicitly declared SSH at a custom port
	s.Binds = []Bind{
		{Service: "ssh-prod-01.tango", LocalAddr: "127.0.0.1:2222", Proto: "tcp"},
		{Service: "inventree.tango", LocalAddr: "127.0.0.1:8000"},
	}
	binds := s.EffectiveBinds("")
	if len(binds) != 2 {
		t.Errorf("operator SSH bind should not be duplicated, got %d binds", len(binds))
	}
	if binds[0].LocalAddr != "127.0.0.1:2222" {
		t.Errorf("operator SSH bind should be preserved: %q", binds[0].LocalAddr)
	}
}

// Sanity: the konmail example we'd write in the operator runbook also
// validates. Catches regressions where we accidentally bake inventree
// assumptions into the schema.
func TestValidate_KonmailExample(t *testing.T) {
	s := &Schmutz{
		Version:      SchmutzSchemaVersion,
		Tenant:       "kontango",
		App:          "konmail",
		Deployment:   "prod-01",
		ZitiIdentity: "machine-4ab2a1a7",
		Binds: []Bind{
			{Service: "konmail.tango", LocalAddr: "127.0.0.1:443", Proto: "tcp"},
			{Service: "konmail-smtp.tango", LocalAddr: "127.0.0.1:25", Proto: "tcp"},
		},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("konmail example invalid: %v", err)
	}
}

// Empty Posture is allowed.
func TestValidate_NoPosture(t *testing.T) {
	s := validInventreeSpec()
	s.Posture = nil
	if err := s.Validate(); err != nil {
		t.Errorf("nil Posture should be allowed: %v", err)
	}
}
