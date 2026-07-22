package security

// ApprovalKey deliberately lives outside the lockfile model. Any change to one
// of its fields invalidates a prior local approval.
type ApprovalKey struct {
	ContentDigest          string
	OverlayDigest          string
	AdapterSecurityVersion string
	PolicyDigest           string
}

type Approval struct {
	SkillQualifiedName string
	Key                ApprovalKey
	ApprovedAt         string
}
