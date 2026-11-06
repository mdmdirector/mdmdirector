package director

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"net/http"

	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/utils"
)

const (
	// WebhookSecretHeader carries the shared secret when the MDM server, or a proxy
	// in front of it, can set a header on the webhook request.
	WebhookSecretHeader = "X-Webhook-Secret"

	// WebhookHMACHeader is the header NanoMDM uses for the Base64 SHA-256 HMAC of
	// the webhook body when it is started with -webhook-hmac-key.
	WebhookHMACHeader = "X-Hmac-Signature"
)

// webhook authentication results, used as the "result" metric label.
const (
	webhookAuthOK           = "ok"
	webhookAuthMissing      = "missing"
	webhookAuthMismatch     = "mismatch"
	webhookAuthUnconfigured = "unconfigured"
)

// WebhookAuth authenticates the MDM server to /webhook.
//
// /webhook is a state-changing route: the events it carries reset devices, update
// inventory and, on an Idle report, can enqueue DeviceLock or EraseDevice. Basic
// auth is the wrong primitive for a machine-to-machine webhook, so a dedicated
// shared secret is used instead and compared in constant time. Two forms are
// accepted, both available from a stock NanoMDM:
//
//   - an HMAC of the request body in the X-Hmac-Signature header, which NanoMDM
//     produces today with -webhook-hmac-key. This is the preferred form: the
//     credential is never sent on the wire.
//   - the secret itself, in X-Webhook-Secret or as the HTTP basic-auth password,
//     which works with a -webhook-url of the form https://user:secret@host/webhook.
//
// STAGING: by default a missing or wrong credential is logged loudly and counted,
// and the request is still served. Director and the MDM server's webhook URL roll
// independently, so rejecting first would drop every event in the window between
// the two deploys. Turn webhook-auth-enforce on once mdmdirector_webhook_auth_total
// shows only result="ok"; from then on anything else is a 401.
func WebhookAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result := authenticateWebhook(r)
		enforced := utils.WebhookAuthEnforced()

		if result == webhookAuthOK {
			recordWebhookAuth(result, "allowed")
			handler(w, r)
			return
		}

		if enforced {
			recordWebhookAuth(result, "rejected")
			ErrorLogger(LogHolder{Message: "Rejecting unauthenticated webhook request: " + result})
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Unauthorised.\n"))
			return
		}

		recordWebhookAuth(result, "allowed")
		WarnLogger(LogHolder{
			Message: "WEBHOOK AUTHENTICATION FAILED (" + result + ") - serving the request anyway " +
				"because webhook-auth-enforce is off. Set the shared secret on both the MDM " +
				"server and Director, then enable enforcement.",
		})
		handler(w, r)
	}
}

func recordWebhookAuth(result, action string) {
	if utils.Prometheus() {
		metrics.WebhookAuth(result, action).Inc()
	}
}

// authenticateWebhook reports which webhook authentication result applies to r. It
// restores r.Body when it has to read it, so the handler still sees the whole body.
func authenticateWebhook(r *http.Request) string {
	if key := utils.WebhookHMACKey(); key != "" {
		return verifyWebhookHMAC(r, []byte(key))
	}

	secret := utils.WebhookSecret()
	if secret == "" {
		return webhookAuthUnconfigured
	}

	presented := r.Header.Get(WebhookSecretHeader)
	if presented == "" {
		if _, pass, ok := r.BasicAuth(); ok {
			presented = pass
		}
	}
	if presented == "" {
		return webhookAuthMissing
	}
	if subtle.ConstantTimeCompare([]byte(presented), []byte(secret)) != 1 {
		return webhookAuthMismatch
	}
	return webhookAuthOK
}

// verifyWebhookHMAC checks the NanoMDM X-Hmac-Signature header over the body.
func verifyWebhookHMAC(r *http.Request, key []byte) string {
	presented := r.Header.Get(WebhookHMACHeader)
	if presented == "" {
		return webhookAuthMissing
	}
	presentedMAC, err := base64.StdEncoding.DecodeString(presented)
	if err != nil {
		return webhookAuthMismatch
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		ErrorLogger(LogHolder{Message: "Could not read the webhook body to verify its signature: " + err.Error()})
		return webhookAuthMismatch
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))

	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	if !hmac.Equal(presentedMAC, mac.Sum(nil)) {
		return webhookAuthMismatch
	}
	return webhookAuthOK
}
