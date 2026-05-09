package verify

import (
	"testing"

	"git.konoss.org/kore/schmutz/controller/internal/service"
)

func TestVerifyDownload_NilRecord(t *testing.T) {
	v := VerifyDownload(nil, AgentConnection{RealIP: "1.2.3.4"})
	if v.Passed {
		t.Error("nil record should fail")
	}
	if v.Reason == "" {
		t.Error("reason should be set")
	}
}

func TestVerifyDownload_IPMatch(t *testing.T) {
	rec := &service.OTTRecord{RealIP: "1.2.3.4", JA4: "t13d...", UA: "curl/8.0"}
	conn := AgentConnection{RealIP: "1.2.3.4", JA4: "different", UA: "different"}
	v := VerifyDownload(rec, conn)
	if !v.Passed {
		t.Errorf("IP match should pass: %s", v.Reason)
	}
	if v.Confidence != "high" {
		t.Errorf("IP match should be high confidence, got %s", v.Confidence)
	}
}

func TestVerifyDownload_JA4Match(t *testing.T) {
	rec := &service.OTTRecord{RealIP: "1.2.3.4", JA4: "t13d1516h2_abc", UA: "curl/8.0"}
	conn := AgentConnection{RealIP: "5.6.7.8", JA4: "t13d1516h2_abc", UA: "different"}
	v := VerifyDownload(rec, conn)
	if !v.Passed {
		t.Errorf("JA4 match should pass: %s", v.Reason)
	}
	if v.Confidence != "medium" {
		t.Errorf("JA4 match should be medium confidence, got %s", v.Confidence)
	}
}

func TestVerifyDownload_UAMatch(t *testing.T) {
	rec := &service.OTTRecord{RealIP: "1.2.3.4", JA4: "t13d...", UA: "schmutz/1.0"}
	conn := AgentConnection{RealIP: "5.6.7.8", JA4: "different", UA: "schmutz/1.0"}
	v := VerifyDownload(rec, conn)
	if !v.Passed {
		t.Errorf("UA match should pass: %s", v.Reason)
	}
	if v.Confidence != "low" {
		t.Errorf("UA match should be low confidence, got %s", v.Confidence)
	}
}

func TestVerifyDownload_NoMatch(t *testing.T) {
	rec := &service.OTTRecord{RealIP: "1.2.3.4", JA4: "t13d...", UA: "curl/8.0"}
	conn := AgentConnection{RealIP: "9.9.9.9", JA4: "completely-different", UA: "wget/1.0"}
	v := VerifyDownload(rec, conn)
	if v.Passed {
		t.Error("no match should fail")
	}
	if v.Confidence != "" {
		t.Errorf("failed match should have empty confidence, got %s", v.Confidence)
	}
}

func TestVerifyDownload_EmptyJA4SkipsJA4Check(t *testing.T) {
	// When JA4 is empty on either side, JA4 match should not fire
	rec := &service.OTTRecord{RealIP: "1.2.3.4", JA4: "", UA: "schmutz/1.0"}
	conn := AgentConnection{RealIP: "5.6.7.8", JA4: "", UA: "other/2.0"}
	v := VerifyDownload(rec, conn)
	// Neither IP nor JA4 match — UA also different — should fail
	if v.Passed {
		t.Error("empty JA4 on both sides with no IP/UA match should fail")
	}
}
