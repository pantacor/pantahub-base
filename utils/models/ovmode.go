package models

// OVMode specifies the type of verification used for device ownership, it could be claim, via tls, manual
type OVMode string

const (
	ClaimVerification   OVMode = "claim"
	TLSVerification     OVMode = "tls"
	ManualVerification  OVMode = "manual"
	DefaultVerification OVMode = "default"
)

func (o OVMode) String() string {
	return string(o)
}

func (o OVMode) IsClaim() bool {
	return o == ClaimVerification
}

func (o OVMode) IsTLS() bool {
	return o == TLSVerification
}

func (o OVMode) IsManual() bool {
	return o == ManualVerification
}

func (o OVMode) IsDefault() bool {
	return o == DefaultVerification
}

func ParseOVT(s string) OVMode {
	switch s {
	case string(ClaimVerification),
		string(TLSVerification),
		string(ManualVerification),
		string(DefaultVerification):
		return OVMode(s)
	default:
		return DefaultVerification
	}
}

// Status represents the current state of owner verification
type Status string

const (
	Pending    Status = "pending"
	InProgress Status = "in_progress"
	Completed  Status = "completed"
	Failed     Status = "failed"
	Unknown    Status = "unknown"
)

func (s Status) String() string {
	return string(s)
}

func (s Status) IsPending() bool {
	return s == Pending
}

func (s Status) IsInProgress() bool {
	return s == InProgress
}

func (s Status) IsCompleted() bool {
	return s == Completed
}

func (s Status) IsFailed() bool {
	return s == Failed
}

func (s Status) IsUnknown() bool {
	return s == Unknown
}

func ParseStatus(s string) Status {
	switch s {
	case string(Pending),
		string(InProgress),
		string(Completed),
		string(Failed),
		string(Unknown):
		return Status(s)
	default:
		return Unknown
	}
}

type OVModeExtension struct {
	// Owner Verification Mode
	Mode OVMode `json:"mode,omitempty" bson:"mode,omitempty"`

	// Status represents the current state of verification
	Status Status `json:"status,omitempty" bson:"status,omitempty"`

	// root of trust is used when the mode is TLS for device verification
	RootOfTrust string `json:"root_of_trust,omitempty" bson:"root_of_trust,omitempty"`
}
