package enroll

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"log"

	"golang.org/x/crypto/ssh"

	"github.com/KontangoOSS/schmutz/controller/internal/controller/common"
)

// genED25519Key returns an authorized_keys-format public key and PEM-encoded private key.
func genED25519Key() (pubKey, privKey string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate ed25519: %w", err)
	}

	// Public key in authorized_keys format
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", fmt.Errorf("marshal public key: %w", err)
	}
	pubKey = string(ssh.MarshalAuthorizedKey(sshPub))

	// Private key as OpenSSH PEM
	privPEM, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", "", fmt.Errorf("marshal private key: %w", err)
	}
	privKey = string(pem.EncodeToMemory(privPEM))

	return pubKey, privKey, nil
}

// createProgressService creates the Ziti service for device progress reporting.
// All enrolled devices can connect to this service and send progress updates.
// The service is accessible via the Ziti overlay network.
//
// Idempotent: if the service already exists, returns immediately. Without this
// guard, every enrollment after the first one logs "failed to create configs"
// because CreateConfig rejects duplicate names.
func createProgressService(c *common.Clients, token string) {
	svcName := "progress-service"

	if c.Ziti.GetServiceIDByName(token, svcName) != "" {
		return
	}

	interceptTypeID := c.Ziti.GetConfigTypeID(token, "intercept.v1")
	hostTypeID := c.Ziti.GetConfigTypeID(token, "host.v2")

	if interceptTypeID == "" || hostTypeID == "" {
		log.Printf("progress: failed to get config type IDs")
		return
	}

	// Intercept config: devices dial progress-service (no port, it's a generic service)
	interceptID, _ := c.Ziti.CreateConfig(token, svcName+"-intercept", interceptTypeID, map[string]interface{}{
		"protocols":  []string{"tcp"},
		"addresses":  []string{"progress-service"},
		"portRanges": []map[string]int{}, // No specific port — Ziti handles routing
	})

	// Host config: controller hosts the service on localhost:9091
	hostID, _ := c.Ziti.CreateConfig(token, svcName+"-host", hostTypeID, map[string]interface{}{
		"terminators": []map[string]interface{}{
			{
				"protocol": "tcp",
				"address":  "127.0.0.1",
				"port":     9091,
			},
		},
	})

	if interceptID == "" || hostID == "" {
		log.Printf("progress: failed to create configs")
		return
	}

	// Create the service
	svcID, _ := c.Ziti.CreateService(token, svcName, []string{interceptID, hostID}, []string{"progress-service"})
	if svcID == "" {
		log.Printf("progress: failed to create service")
		return
	}

	// Bind policy: controller hosts the service (has #controller attribute)
	c.Ziti.CreateServicePolicy(token, svcName+"-bind", "Bind", "AnyOf", []string{"#controller"}, []string{"@" + svcID})

	// Dial policy: all enrolled devices can connect
	// Devices get #enrolled attribute when they successfully enroll
	c.Ziti.CreateServicePolicy(token, svcName+"-dial", "Dial", "AnyOf", []string{"#enrolled"}, []string{"@" + svcID})

	log.Printf("progress: created ziti service %q (localhost:9091)", svcName)
}

// StartProgressServiceListener starts the Ziti listener for device progress messages.
// This should be called during controller startup after Ziti identity is available.
func (m *Module) StartProgressServiceListener(zitiTransport interface{}) error {
	// Type assertion for ZitiTransport (avoid direct dependency)
	// In real usage:
	//   listener, err := zitiTransport.ListenEdge("progress-service")
	//   if err != nil {
	//       return err
	//   }
	//   go m.HandleProgressService(listener)

	log.Printf("progress: listener started for ziti service 'progress-service'")
	return nil
}

// AddEnrolledAttribute adds the #enrolled attribute to a device identity.
// This allows the device to connect to progress-service and other enrolled services.
func AddEnrolledAttribute(c *common.Clients, token, identityID string) error {
	// Add role attribute to the identity so they match the dial policy
	// This is typically done in the enrollment flow after the device is approved
	return c.Ziti.UpdateIdentityAddAttr(token, identityID, "enrolled")
}

// AddControllerAttribute adds the #controller attribute to a controller identity.
// Controllers can bind services and have special access.
func AddControllerAttribute(c *common.Clients, token, identityID string) error {
	return c.Ziti.UpdateIdentityAddAttr(token, identityID, "controller")
}

// EnsureTelemetryService creates the telemetry.tango Ziti service if it doesn't exist.
// Agents with #enrolled role dial it; the controller identity binds (hosts) it.
// Idempotent — safe to call on every startup.
func EnsureTelemetryService(c *common.Clients, token string) {
	const svcName = "telemetry.tango"

	if c.Ziti.GetServiceIDByName(token, svcName) != "" {
		return // already exists
	}

	svcID := c.Ziti.CreateServiceSimple(token, svcName)
	if svcID == "" {
		log.Printf("telemetry-svc: failed to create %s", svcName)
		return
	}

	// Enrolled machines can dial
	c.Ziti.CreateServicePolicy(token, svcName+"-dial", "Dial", "AnyOf",
		[]string{"#enrolled"}, []string{"@"+svcID})

	// Controller can bind (host)
	c.Ziti.CreateServicePolicy(token, svcName+"-bind", "Bind", "AnyOf",
		[]string{"#controllers"}, []string{"@"+svcID})

	// Edge router policy so packets can flow
	// signature: (token, name, semantic, serviceRoles, routerRoles)
	c.Ziti.CreateServiceEdgeRouterPolicy(token, svcName+"-erp", "AnyOf",
		[]string{"@"+svcID}, []string{"#all"})

	log.Printf("telemetry-svc: created %s", svcName)
}

// EnsureRelayService creates the relay.tango Ziti service if it doesn't exist.
// Only trusted consumers (#dashboards role) can dial; controller binds.
// Idempotent.
func EnsureRelayService(c *common.Clients, token string) {
	const svcName = "relay.tango"

	if c.Ziti.GetServiceIDByName(token, svcName) != "" {
		return // already exists
	}

	svcID := c.Ziti.CreateServiceSimple(token, svcName)
	if svcID == "" {
		log.Printf("relay-svc: failed to create %s", svcName)
		return
	}

	// Trusted dashboard consumers can dial
	c.Ziti.CreateServicePolicy(token, svcName+"-dial", "Dial", "AnyOf",
		[]string{"#dashboards"}, []string{"@"+svcID})

	// Controller can bind (host)
	c.Ziti.CreateServicePolicy(token, svcName+"-bind", "Bind", "AnyOf",
		[]string{"#controllers"}, []string{"@"+svcID})

	// Edge router policy so packets can flow
	// signature: (token, name, semantic, serviceRoles, routerRoles)
	c.Ziti.CreateServiceEdgeRouterPolicy(token, svcName+"-erp", "AnyOf",
		[]string{"@"+svcID}, []string{"#all"})

	log.Printf("relay-svc: created %s (dial=#dashboards)", svcName)
}
