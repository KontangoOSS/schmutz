package main

import (
	"flag"
	"fmt"
	"os"
)

// cmdSignup is the human account creation command.
//
// NOT YET IMPLEMENTED — endpoint does not exist on the controller.
//
// When implemented, this command creates a full human account on the platform:
//   - A Zitadel user (identity provider account, SSO login)
//   - A Bao entity (secrets namespace, credential storage)
//   - An account record associating the human with their devices
//
// This is the true "welcome to the program" step. It runs once per human,
// not once per device. After signup, the user can run `schmutz join` on
// each of their devices to associate them with their account.
//
// Planned flow:
//  1. Collect: email, display name, optional invite code
//  2. POST /signup on the controller
//  3. Controller creates Zitadel user → sends verification email
//  4. Controller creates Bao entity + policies for the user's namespace
//  5. Controller returns account ID + claim URL
//  6. User verifies email → account is active
//  7. Future `schmutz join` calls can reference the account ID to associate devices
//
// The join and enroll commands work without signup — devices can be enrolled
// independently and associated with an account later via the claim URL.
func cmdSignup(args []string) {
	fs := flag.NewFlagSet("signup", flag.ExitOnError)
	_ = fs.String("url", "https://ctrl.konoss.org", "controller URL")
	_ = fs.String("email", "", "email address for the new account")
	_ = fs.String("name", "", "display name")
	_ = fs.String("invite", "", "invite code (if required)")
	fs.Parse(args)

	fmt.Fprintln(os.Stderr, "schmutz signup: not yet implemented")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  This command will create a full platform account (Zitadel + Bao + device namespace).")
	fmt.Fprintln(os.Stderr, "  The /signup endpoint does not yet exist on the controller.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  To enroll a device without an account: schmutz join <url> --slug <name>")
	os.Exit(1)
}
