package service

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func skip(t *testing.T) {
	if os.Getenv("BAO_ADDR") == "" || os.Getenv("BAO_TOKEN") == "" ||
		os.Getenv("ZITI_CTRL_ADDR") == "" || os.Getenv("ZITI_ADMIN_PASS") == "" {
		t.Skip("Set BAO_ADDR, BAO_TOKEN, ZITI_CTRL_ADDR, ZITI_ADMIN_PASS")
	}
}
func bao(t *testing.T) *StoreService {
	skip(t)
	s, _ := NewStoreService(os.Getenv("BAO_ADDR"), os.Getenv("BAO_TOKEN"))
	return s
}
func ident(t *testing.T) *IdentityService {
	skip(t)
	s, _ := NewIdentityService(os.Getenv("BAO_ADDR"), os.Getenv("BAO_TOKEN"))
	return s
}
func zitiSvc(t *testing.T) (*ZitiService, string) {
	skip(t)
	z := &ZitiService{Addr: os.Getenv("ZITI_CTRL_ADDR"), User: "admin", Pass: os.Getenv("ZITI_ADMIN_PASS")}
	tok, _ := z.Authenticate()
	return z, tok
}
func ts() string { return fmt.Sprintf("%d", time.Now().UnixNano()%1000000) }

// === BAO KV ===
func TestBao_KV(t *testing.T) {
	s := bao(t)
	k := "_t/kv/" + ts()
	defer s.Delete("secret", k)
	s.Put("secret", k, map[string]interface{}{"v": 1})
	d, _ := s.Get("secret", k)
	if d == nil {
		t.Fatal("read")
	}
	s.Delete("secret", k)
	d2, _ := s.Get("secret", k)
	if d2 != nil {
		t.Error("delete")
	}
}
func TestBao_List(t *testing.T) {
	s := bao(t)
	k := "_t/l/" + ts()
	defer s.Delete("secret", k)
	s.Put("secret", k, map[string]interface{}{"a": 1})
	l, _ := s.List("secret", "_t/l/")
	if len(l) == 0 {
		t.Error("list")
	}
}
func TestBao_Machine(t *testing.T) {
	s := bao(t)
	id := "_tm" + ts()
	defer s.Delete("secret", "identities/machines/"+id)
	defer s.Delete("secret", "identities/machines/by-hostname/_tmh")
	s.PutMachine(id, map[string]interface{}{"hostname": "_tmh", "nickname": "n", "id": id})
	d, _ := s.GetMachine(id)
	if d == nil {
		t.Fatal("get")
	}
	_, n, ok := s.LookupByHostname("_tmh")
	if !ok || n != "n" {
		t.Error("lookup")
	}
}
func TestBao_Ban(t *testing.T) {
	s := bao(t)
	b := "_tb" + ts()
	defer s.UnbanDevice(b)
	time.Sleep(100 * time.Millisecond)
	if s.IsBanned(b) {
		t.Error("pre")
	}
	s.BanDevice(b, "t")
	time.Sleep(100 * time.Millisecond)
	if !s.IsBanned(b) {
		t.Error("ban")
	}
	s.UnbanDevice(b)
	if s.IsBanned(b) {
		t.Error("unban")
	}
}
func TestBao_Profile(t *testing.T) {
	s := bao(t)
	n := "_tp" + ts()
	defer s.Delete("secret", "identities/profiles/"+n)
	s.PutProfile(n, map[string]interface{}{"a": 1})
	p, _ := s.GetProfile(n)
	if p == nil {
		t.Fatal("get")
	}
	l, _ := s.ListProfiles()
	if l == nil {
		t.Error("list")
	}
}
func TestBao_Known(t *testing.T) {
	s := bao(t)
	n := "_tk" + ts()
	defer s.Delete("secret", "identities/known/"+n)
	s.Put("secret", "identities/known/"+n, map[string]interface{}{"p": "w"})
	d, _ := s.GetKnownMachine(n)
	if d == nil {
		t.Fatal("get")
	}
	d2, _ := s.GetKnownMachine("x")
	if d2 != nil {
		t.Error("nil")
	}
}
func TestBao_Audit(t *testing.T) {
	s := bao(t)
	if err := s.WriteAuditLog("_ta", "test", nil); err != nil {
		t.Fatal(err)
	}
}
func TestBao_Controller(t *testing.T) {
	s := bao(t)
	d, _ := s.GetControllerInfo("ctrl-1")
	if d == nil {
		t.Fatal("nil")
	}
}
func TestBao_EntityKV(t *testing.T) {
	s := bao(t)
	id := "_te" + ts()
	defer s.Delete("secret", "identities/entities/"+id)
	s.PutEntity(id, map[string]interface{}{"n": "t"})
	d, _ := s.GetEntity(id)
	if d == nil {
		t.Fatal("get")
	}
}

// === BAO SYSTEM ===
func TestBao_Seal(t *testing.T) {
	s := bao(t)
	st, _ := s.SealStatus()
	if st["sealed"] != false {
		t.Error("sealed")
	}
}
func TestBao_Leader(t *testing.T) {
	s := bao(t)
	l, _ := s.Leader()
	if l == nil {
		t.Error("nil")
	}
}
func TestBao_Policy(t *testing.T) {
	s := bao(t)
	n := "_tp" + ts()
	defer s.DeletePolicy(n)
	s.CreatePolicy(n, `path "secret/*" {capabilities=["read"]}`)
	r, _ := s.GetPolicy(n)
	if r == "" {
		t.Error("get")
	}
	l, _ := s.ListPolicies()
	if l == nil {
		t.Error("list")
	}
	s.DeletePolicy(n)
}
func TestBao_Token(t *testing.T) {
	s := bao(t)
	tok, _ := s.CreateToken([]string{"default"}, "5m", nil)
	if tok == "" {
		t.Fatal("create")
	}
	defer s.RevokeToken(tok)
	i, _ := s.LookupToken(tok)
	if i == nil {
		t.Error("lookup")
	}
	s.RevokeToken(tok)
}
func TestBao_AppRole(t *testing.T) {
	s := bao(t)
	n := "_tr" + ts()
	defer s.DeleteAppRole(n)
	s.CreateAppRole(n, []string{"default"}, "1h")
	rid, _ := s.GetAppRoleID(n)
	if rid == "" {
		t.Error("rid")
	}
	sid, _ := s.CreateAppRoleSecretID(n, nil)
	if sid == "" {
		t.Error("sid")
	}
	l, _ := s.ListAppRoles()
	if l == nil {
		t.Error("list")
	}
	s.DeleteAppRole(n)
}
func TestBao_OIDC(t *testing.T) { s := bao(t); s.GetOIDCConfig() }

// === BAO IDENTITY ===
func TestIdent_Entity(t *testing.T) {
	s := ident(t)
	n := "_te" + ts()
	id, _ := s.CreateEntity(n, map[string]string{"t": "1"}, nil)
	if id == "" {
		t.Fatal("create")
	}
	time.Sleep(200 * time.Millisecond)
	defer s.DeleteEntity(id)
	e, _ := s.GetEntityByID(id)
	if e == nil {
		t.Error("get")
	}
	s.UpdateEntityMetadata(id, map[string]string{"t": "2"})
	s.ListEntities()
	s.DeleteEntity(id)
}
func TestIdent_Alias(t *testing.T) {
	s := ident(t)
	eid, _ := s.CreateEntity("_ta"+ts(), nil, nil)
	if eid == "" {
		t.Skip("no entity")
	}
	defer s.DeleteEntity(eid)
	acc, _ := s.GetAuthAccessor("token/")
	aid, _ := s.CreateAlias(eid, "_tal"+ts(), acc, nil)
	if aid == "" {
		t.Fatal("create")
	}
	defer s.DeleteAlias(aid)
	a, _ := s.GetAlias(aid)
	if a == nil {
		t.Error("get")
	}
	s.UpdateAlias(aid, map[string]string{"u": "1"})
	s.DeleteAlias(aid)
}
func TestIdent_Group(t *testing.T) {
	s := ident(t)
	n := "_tg" + ts()
	gid, _ := s.CreateGroup(n, nil, nil, nil)
	if gid == "" {
		t.Fatal("create")
	}
	defer s.DeleteGroup(gid)
	g, _ := s.GetGroup(n)
	if g == nil {
		t.Error("get")
	}
	s.ListGroups()
	s.DeleteGroup(gid)
}
func TestIdent_Membership(t *testing.T) {
	s := ident(t)
	sf := ts()
	eid, _ := s.CreateEntity("_tm"+sf, nil, nil)
	defer s.DeleteEntity(eid)
	gid, _ := s.CreateGroup("_tmg"+sf, nil, nil, nil)
	defer s.DeleteGroup(gid)
	s.AddGroupMember("_tmg"+sf, eid)
	s.RemoveGroupMember("_tmg"+sf, eid)
}
func TestIdent_Accessor(t *testing.T) {
	s := ident(t)
	a, _ := s.GetAuthAccessor("token/")
	if a == "" {
		t.Error("token")
	}
	a2, _ := s.GetAuthAccessor("approle/")
	if a2 == "" {
		t.Error("approle")
	}
}

// === ZITI ===
func TestZiti_Auth(t *testing.T) {
	_, tok := zitiSvc(t)
	if len(tok) < 10 {
		t.Error("short")
	}
}
func TestZiti_CfgTypes(t *testing.T) {
	z, tok := zitiSvc(t)
	for _, c := range []string{"intercept.v1", "host.v1", "host.v2"} {
		if z.GetConfigTypeID(tok, c) == "" {
			t.Errorf("%s", c)
		}
	}
}
func TestZiti_Identity(t *testing.T) {
	z, _ := zitiSvc(t)
	n := "_tzi" + ts()
	id, jwt, _ := z.CreateIdentity(n, []string{"quarantine"}, map[string]interface{}{"t": 1})
	if id == "" || jwt == "" {
		t.Fatal("create")
	}
	tok, _ := z.Authenticate()
	defer z.DeleteIdentity(tok, id)
	_, a, ad, _ := z.GetIdentityByName(tok, n)
	if len(a) == 0 {
		t.Error("attrs")
	}
	if ad["t"] == nil {
		t.Error("appdata")
	}
	z.UpdateIdentity(tok, id, []string{"web"})
	z.UpdateIdentityAppData(tok, id, map[string]interface{}{"t": 2})
	z.GetIdentityPolicies(tok, id)
	z.ListIdentities(tok, 5)
	z.DeleteIdentity(tok, id)
}
func TestZiti_Service(t *testing.T) {
	z, tok := zitiSvc(t)
	s := ts()
	iID, _ := z.CreateConfig(tok, "_ti"+s, z.GetConfigTypeID(tok, "intercept.v1"), map[string]interface{}{"protocols": []string{"tcp"}, "addresses": []string{"_t.tango"}, "portRanges": []map[string]int{{"low": 9999, "high": 9999}}})
	defer z.DeleteConfig(tok, iID)
	hID, _ := z.CreateConfig(tok, "_th"+s, z.GetConfigTypeID(tok, "host.v2"), map[string]interface{}{"terminators": []map[string]interface{}{{"protocol": "tcp", "address": "127.0.0.1", "port": 9999}}})
	defer z.DeleteConfig(tok, hID)
	sID, _ := z.CreateService(tok, "_ts"+s, []string{iID, hID}, []string{"_t"})
	defer z.DeleteService(tok, sID)
	z.GetService(tok, sID)
	z.GetConfig(tok, iID)
	z.CreateServicePolicy(tok, "_tb"+s, "Bind", "AnyOf", []string{"#quarantine"}, []string{"@" + sID})
	z.CreateServicePolicy(tok, "_td"+s, "Dial", "AnyOf", []string{"#admin-users"}, []string{"@" + sID})
	bID, _ := z.FindByName(tok, "service-policies", "_tb"+s)
	dID, _ := z.FindByName(tok, "service-policies", "_td"+s)
	defer z.DeleteServicePolicy(tok, bID)
	defer z.DeleteServicePolicy(tok, dID)
	z.GetServicePolicy(tok, bID)
	z.ListServicePolicies(tok)
	z.ListConfigs(tok)
	z.ListServices(tok)
}
func TestZiti_Routers(t *testing.T) {
	z, tok := zitiSvc(t)
	r, _ := z.ListRouters(tok)
	if len(r) == 0 {
		t.Error("none")
	}
}
func TestZiti_Terminators(t *testing.T) { z, tok := zitiSvc(t); z.ListTerminators(tok, "") }
func TestZiti_ERPolicies(t *testing.T) {
	z, tok := zitiSvc(t)
	p, _ := z.ListEdgeRouterPolicies(tok)
	if len(p) == 0 {
		t.Error("none")
	}
}
func TestZiti_SERPolicies(t *testing.T) { z, tok := zitiSvc(t); z.ListServiceEdgeRouterPolicies(tok) }
func TestZiti_Enrollments(t *testing.T) { z, tok := zitiSvc(t); z.ListEnrollments(tok) }
func TestZiti_RoleAttrs(t *testing.T) {
	z, tok := zitiSvc(t)
	a, _ := z.ListRoleAttributes(tok, "identity")
	if len(a) == 0 {
		t.Error("none")
	}
}
func TestZiti_CAs(t *testing.T)      { z, tok := zitiSvc(t); z.ListCAs(tok) }
func TestZiti_Sessions(t *testing.T) { z, tok := zitiSvc(t); z.ListSessions(tok) }
func TestZiti_Posture(t *testing.T)  { z, tok := zitiSvc(t); z.ListPostureChecks(tok) }
func TestZiti_Find(t *testing.T) {
	z, tok := zitiSvc(t)
	id, _ := z.FindByName(tok, "services", "quarantine-ssh")
	if id == "" {
		t.Error("not found")
	}
}

// === SECURITY ===
func TestSec_Rate(t *testing.T) {
	s := NewSecurityService(nil)
	for i := 0; i < 5; i++ {
		s.RateLimit("1.2.3.4", 5, time.Minute)
	}
	if !s.RateLimit("1.2.3.4", 5, time.Minute) {
		t.Error("limit")
	}
	if s.RateLimit("5.6.7.8", 5, time.Minute) {
		t.Error("other")
	}
}
func TestSec_Ban(t *testing.T) {
	s := bao(t)
	sec := NewSecurityService(s)
	b := "_tsb" + ts()
	defer s.UnbanDevice(b)
	if sec.CheckBanned(b, "", nil) {
		t.Error("pre")
	}
	s.BanDevice(b, "t")
	if !sec.CheckBanned(b, "", nil) {
		t.Error("ban")
	}
	s.UnbanDevice(b)
}
func TestSec_FP(t *testing.T) {
	s := bao(t)
	sec := NewSecurityService(s)
	id := "_tfp" + ts()
	defer s.Delete("secret", "identities/machines/"+id)
	defer s.Delete("secret", "identities/machines/by-hostname/_tfp")
	s.PutMachine(id, map[string]interface{}{"hostname": "_tfp", "nickname": "f", "id": id, "hardware": map[string]interface{}{"fingerprint": "abc"}})
	if !sec.ValidateFingerprint(id, "abc") {
		t.Error("match")
	}
	if sec.ValidateFingerprint(id, "wrong") {
		t.Error("wrong")
	}
}

// === METRICS ===
func TestMetrics_Full(t *testing.T) {
	z, tok := zitiSvc(t)
	s := bao(t)
	m := NewMetricsService(z, s)
	status := m.FullStatus(tok)
	if status["ziti"] == nil {
		t.Error("no ziti")
	}
	if status["bao"] == nil {
		t.Error("no bao")
	}
	if status["machine"] == nil {
		t.Error("no machine")
	}
	if status["latency"] == nil {
		t.Error("no latency")
	}
	t.Logf("status: ziti=%v bao=%v", status["ziti"], status["bao"])
}
