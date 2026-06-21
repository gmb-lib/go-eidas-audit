package eidas

// Event types for eIDAS-audit material signing events. These
// are the canonical event_type values written into the hash-chain; keeping them
// here is what makes the trail consistent across producers.
const (
	// EventDocumentUploaded — a document entered the system (also a GDPR access).
	EventDocumentUploaded = "document.uploaded"
	// EventDocumentPreviewed — a document/envelope was opened for review (the
	// "what-you-saw-is-what-you-signed" anchor).
	EventDocumentPreviewed = "document.previewed"
	// EventConsentCaptured — consent / intent to sign captured against an exact
	// document hash.
	EventConsentCaptured = "signing.consent"
	// EventAuthAssurance — authentication assurance established at signing time.
	EventAuthAssurance = "signing.assurance"
	// EventSigningInitiated — signing started for a slot via a chosen method.
	EventSigningInitiated = "signing.initiated"
	// EventProviderRedirect — the signer was redirected to LVRTC / Entrust.
	EventProviderRedirect = "signing.redirect"
	// EventProviderCallback — the LVRTC / Entrust callback returned.
	EventProviderCallback = "signing.callback"
	// EventSignatureApplied — a signature was applied to a slot.
	EventSignatureApplied = "signing.applied"
	// EventValidationPerformed — a validation pass was performed on a document.
	EventValidationPerformed = "signing.validation"
	// EventEnvelopeTransition — an envelope changed lifecycle state.
	EventEnvelopeTransition = "envelope.transition"
	// EventCoSignerInvited — one party's action caused processing of another's
	// data (also a GDPR access).
	EventCoSignerInvited = "envelope.cosigner_invited"
	// EventDocumentDownloaded — a signed document / evidence artefact was
	// downloaded.
	EventDocumentDownloaded = "document.downloaded"
	// EventRetentionPurge — a retention sweep deleted material; the fact of
	// deletion is itself retained.
	EventRetentionPurge = "retention.purge"
)

// Resource types the events concern.
const (
	ResourceDocument = "document"
	ResourceEnvelope = "envelope"
	ResourceIdentity = "identity"
)

// Attribute keys. Defined as constants so every producer writes the same shape
// into the chain; values are lean references only.
const (
	AttrContentHash      = "content_hash"
	AttrMIME             = "mime"
	AttrSize             = "size"
	AttrSlot             = "slot"
	AttrDocumentHash     = "document_hash"
	AttrMethod           = "method"
	AttrLevelOfAssurance = "loa"
	AttrBindingOutcome   = "binding_outcome"
	AttrStepUp           = "step_up"
	AttrInputType        = "input_type"
	AttrSessionID        = "session_id"
	AttrProvider         = "provider"
	AttrStateRef         = "state_ref"
	AttrSignatureFormat  = "signature_format"
	AttrSignatureLevel   = "signature_level"
	AttrSimpleSignRef    = "simplesign_ref"
	// AttrBaselineConfirmed records that the backend confirmed the expected
	// AdES baseline level — B-LT (qualified signature timestamp + embedded
	// OCSP/CRL; archive timestamps are applied only later, by the preservation
	// service, via /addArchive).
	AttrBaselineConfirmed     = "baseline_confirmed"
	AttrQualifiedTimestampRef = "qualified_timestamp_ref"
	AttrValidationPolicy      = "validation_policy"
	AttrReportLevel           = "report_level"
	AttrFormat                = "format"
	AttrValidationReportRef   = "validation_report_ref"
	AttrFromState             = "from_state"
	AttrToState               = "to_state"
	AttrReason                = "reason"
	AttrWhat                  = "what"
	AttrCount                 = "count"
	AttrBasis                 = "basis"
	AttrRetainedUnderHold     = "retained_under_hold"
	AttrFileRef               = "file_ref"
)

// SignatureFormat is the container/profile a signature was applied in.
type SignatureFormat string

// Signature formats supported by the platform. The signing backend produces
// B-LT baseline signatures (see AttrBaselineConfirmed). FormatASiCE denotes a
// XAdES signature delivered in an ASiC-E container — recorded as the
// container-level format for consistency with the delivered artefact.
const (
	FormatPAdES SignatureFormat = "PAdES"
	FormatXAdES SignatureFormat = "XAdES"
	FormatASiCE SignatureFormat = "ASiC-E"
)

// SignatureLevel is the legal level of the applied signature/seal.
type SignatureLevel string

// Signature levels.
const (
	LevelQES  SignatureLevel = "QES"  // qualified electronic signature
	LevelAdES SignatureLevel = "AdES" // advanced electronic signature
	LevelSeal SignatureLevel = "SEAL" // qualified electronic seal
)

// ValidationPolicy selects the validation profile used for a report.
type ValidationPolicy string

// Validation policies.
const (
	PolicyLV     ValidationPolicy = "lv"
	PolicyEU     ValidationPolicy = "eu"
	PolicyLegacy ValidationPolicy = "legacy"
)

// InputType is what was fed into the signing call.
type InputType string

// Signing input types.
const (
	InputHash InputType = "hash"
	InputFile InputType = "file"
	InputPDF  InputType = "pdf"
)

// SigningProvider identifies the remote trust service the signer was sent to.
type SigningProvider string

// Signing providers.
const (
	ProviderLVRTC   SigningProvider = "lvrtc"
	ProviderEntrust SigningProvider = "entrust"
)
