package metrics

import "go.opentelemetry.io/otel/attribute"

// Attribute-key constants. Defined here so call-sites pick the same
// keys and so a typo in one place doesn't fragment a metric.
const (
	keyFormat  = "format"
	keyOutcome = "outcome"
	keyRoute   = "route"
	keyQuery   = "query"
	keyKind    = "kind"
)

// Outcome enums used by AuthAttempts / RefreshAttempts. Keep this
// list small — high-cardinality outcomes blow up Prometheus storage.
const (
	OutcomeSuccess       = "success"
	OutcomeInvalidToken  = "invalid_token"
	OutcomeStoreError    = "store_error"
	OutcomeBadRequest    = "bad_request"
	OutcomeMisconfigured = "misconfigured"
	OutcomeExpired       = "expired"
	OutcomeRevoked       = "revoked"
	OutcomeTheftDetected = "theft_detected"
	OutcomeNotFound      = "not_found"
	OutcomeIssueFailed   = "issue_failed"
)

// JWT-verify "kind" enum.
const (
	KindAccess  = "access"
	KindRefresh = "refresh"
)

// Format enum used by Events* counters.
const (
	FormatBatch = "batch"
	FormatCSV   = "csv"
	FormatJSON  = "json"
)

func attrFormat(v string) attribute.KeyValue  { return attribute.String(keyFormat, v) }
func attrOutcome(v string) attribute.KeyValue { return attribute.String(keyOutcome, v) }
func attrRoute(v string) attribute.KeyValue   { return attribute.String(keyRoute, v) }
func attrQuery(v string) attribute.KeyValue   { return attribute.String(keyQuery, v) }
func attrKind(v string) attribute.KeyValue    { return attribute.String(keyKind, v) }
