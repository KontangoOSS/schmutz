package enroll

// enroll_ws_test.go — tests for the WebSocket enrollment flow.
//
// generateCSR and injectPrivateKey have been removed: the Ziti SDK's enroll.Enroll()
// handles key generation and identity assembly internally. Those functions are no
// longer part of the to-go enrollment path.
//
// The SDK-level enrollment (enrollWithZitiSDK) requires a live Ziti controller and
// a valid OTT JWT, so it is tested in integration tests, not unit tests here.
//
// The wsEnrollMsg struct and touchStart helper are exercised indirectly by RegisterWS,
// which is also an integration-level concern. Unit-testable surface here is minimal
// by design — the critical logic lives in the Ziti SDK.
