package db

import "time"

// Models returns the GORM mapping of the PostgreSQL tables.
// Schema changes are applied by SQL migrations, not AutoMigrate.
func Models() []any {
	return []any{
		Project{}, Environment{}, APIKey{}, SenderIdentity{}, Message{}, MessageEvent{},
		Suppression{}, RateLimitBucket{}, SandboxCapture{}, WorkerHeartbeat{},
		User{}, Session{}, AuditLog{}, Setting{}, Alert{},
		Template{}, TemplateVersion{}, TemplateContent{},
		WebhookEndpoint{}, WebhookDelivery{},
		InboundAddress{}, InboundSpool{}, InboundMessage{}, InboundAttachment{},
		Bounce{}, DNSCheck{}, MessageAttachment{}, Domain{}, PasswordReset{}, AlertChannel{},
	}
}

type Project struct {
	ID                 string `gorm:"type:uuid;primaryKey"`
	Name               string `gorm:"uniqueIndex"`
	Enabled            bool
	SendPerMinute      int
	SendPerDay         int
	RecipientPerMinute int
	RecipientPerHour   int
	CreatedAt          time.Time
}

func (Project) TableName() string { return "projects" }

type Environment struct {
	ID                 string `gorm:"type:uuid;primaryKey"`
	ProjectID          string `gorm:"type:uuid"`
	Mode               string
	ApprovedOrigins    string `gorm:"type:text[]"`
	RestrictRecipients bool
	RecipientAllowlist string `gorm:"type:text[]"`
	Enabled            bool
}

func (Environment) TableName() string { return "environments" }

type APIKey struct {
	ID            string `gorm:"type:uuid;primaryKey"`
	Prefix        string `gorm:"uniqueIndex"`
	SecretHash    []byte
	ProjectID     string `gorm:"type:uuid"`
	EnvironmentID string `gorm:"type:uuid"`
	Scopes        string `gorm:"type:text[]"`
	Label         string
	CreatedAt     time.Time
	ExpiresAt     *time.Time
	RevokedAt     *time.Time
	IPAllowlist   string `gorm:"column:ip_allowlist;type:text[]"`
	RatePerMinute int
	LastUsedAt    *time.Time
}

func (APIKey) TableName() string { return "api_keys" }

type SenderIdentity struct {
	ID            string `gorm:"type:uuid;primaryKey"`
	ProjectID     string `gorm:"type:uuid"`
	EnvironmentID string `gorm:"type:uuid"`
	Email         string
	DisplayName   string
	CreatedAt     time.Time
}

func (SenderIdentity) TableName() string { return "sender_identities" }

type Message struct {
	ID                string `gorm:"type:uuid;primaryKey"`
	ProjectID         string `gorm:"type:uuid"`
	EnvironmentID     string `gorm:"type:uuid"`
	APIKeyID          *string
	Template          string
	TemplateVersion   int
	State             string
	PayloadEnc        []byte
	RecipientHash     []byte
	RequestHMAC       []byte `gorm:"column:request_hmac"`
	IdempotencyKey    string
	Metadata          []byte `gorm:"type:jsonb"`
	ExpiresAt         time.Time
	Attempts          int
	NextAttemptAt     time.Time
	LeaseToken        *string
	LeaseExpiresAt    *time.Time
	SMTPReply         string
	SMTPMessageID     string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	SubmittedAt       *time.Time
	CompletedAt       *time.Time
	Locale            string
	TemplateVersionID *string
	BounceState       string
	Priority          string
	SendAt            *time.Time
	ReplyTo           string
	CC                string `gorm:"type:text[]"`
	BCC               string `gorm:"type:text[]"`
}

func (Message) TableName() string { return "messages" }

type MessageEvent struct {
	ID         int64 `gorm:"primaryKey"`
	MessageID  string
	EventType  string
	ReasonCode string
	CreatedAt  time.Time
}

func (MessageEvent) TableName() string { return "message_events" }

type Suppression struct {
	ID              string `gorm:"type:uuid;primaryKey"`
	ProjectID       string `gorm:"type:uuid"`
	EnvironmentID   string `gorm:"type:uuid"`
	RecipientHash   []byte
	RecipientMasked string
	Reason          string
	CreatedAt       time.Time
	Source          string
	BounceID        *string
}

func (Suppression) TableName() string { return "suppressions" }

type RateLimitBucket struct {
	ScopeKey    string    `gorm:"primaryKey"`
	WindowStart time.Time `gorm:"primaryKey"`
	Count       int
}

func (RateLimitBucket) TableName() string { return "rate_limit_buckets" }

type SandboxCapture struct {
	MessageID string `gorm:"type:uuid;primaryKey"`
	RawEnc    []byte
	ExpiresAt time.Time
}

func (SandboxCapture) TableName() string { return "sandbox_captures" }

type WorkerHeartbeat struct {
	WorkerID string `gorm:"primaryKey"`
	LastSeen time.Time
	Role     string
}

func (WorkerHeartbeat) TableName() string { return "worker_heartbeats" }

type User struct {
	ID           string `gorm:"type:uuid;primaryKey"`
	Email        string `gorm:"uniqueIndex"`
	PasswordHash string
	Role         string
	Disabled     bool
	LastLoginAt  *time.Time
	CreatedAt    time.Time
}

func (User) TableName() string { return "users" }

type Session struct {
	TokenHash []byte `gorm:"primaryKey"`
	UserID    string
	ExpiresAt time.Time
	IP        string
	UserAgent string
	CreatedAt time.Time
}

func (Session) TableName() string { return "sessions" }

type AuditLog struct {
	ID         int64 `gorm:"primaryKey"`
	At         time.Time
	UserID     *string
	APIKeyID   *string
	Actor      string
	Action     string
	TargetType string
	TargetID   string
	Details    []byte `gorm:"type:jsonb"`
}

func (AuditLog) TableName() string { return "audit_log" }

type Setting struct {
	Key       string `gorm:"primaryKey"`
	Value     []byte `gorm:"type:jsonb"`
	UpdatedAt time.Time
}

func (Setting) TableName() string { return "settings" }

type Alert struct {
	ID             int64 `gorm:"primaryKey"`
	Kind           string
	Summary        string
	Details        []byte `gorm:"type:jsonb"`
	CreatedAt      time.Time
	AcknowledgedAt *time.Time
	AcknowledgedBy string
}

func (Alert) TableName() string { return "alerts" }

type Template struct {
	ID                string `gorm:"type:uuid;primaryKey"`
	ProjectID         *string
	Slug              string
	Description       string
	DefaultTTLSeconds int
	MaxTTLSeconds     int
	Archived          bool
	CreatedAt         time.Time
}

func (Template) TableName() string { return "templates" }

type TemplateVersion struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	TemplateID     string
	Version        int
	VariableSchema []byte `gorm:"type:jsonb"`
	PublishedAt    *time.Time
	CreatedAt      time.Time
}

func (TemplateVersion) TableName() string { return "template_versions" }

type TemplateContent struct {
	VersionID string `gorm:"primaryKey"`
	Locale    string `gorm:"primaryKey"`
	Subject   string
	HTMLBody  string
	TextBody  string
}

func (TemplateContent) TableName() string { return "template_contents" }

type WebhookEndpoint struct {
	ID                  string `gorm:"type:uuid;primaryKey"`
	ProjectID           string `gorm:"type:uuid"`
	EnvironmentID       string `gorm:"type:uuid"`
	URL                 string
	SecretEnc           []byte
	Events              string `gorm:"type:text[]"`
	Enabled             bool
	ConsecutiveFailures int
	DisabledReason      string
	CreatedAt           time.Time
}

func (WebhookEndpoint) TableName() string { return "webhook_endpoints" }

type WebhookDelivery struct {
	ID               string `gorm:"type:uuid;primaryKey"`
	EndpointID       string
	EventType        string
	Payload          []byte `gorm:"type:jsonb"`
	State            string
	Attempts         int
	NextAttemptAt    time.Time
	LeaseToken       *string
	LeaseExpiresAt   *time.Time
	LastResponseCode *int
	LastError        string
	CreatedAt        time.Time
	CompletedAt      *time.Time
}

func (WebhookDelivery) TableName() string { return "webhook_deliveries" }

type InboundAddress struct {
	ID                string `gorm:"type:uuid;primaryKey"`
	ProjectID         string `gorm:"type:uuid"`
	EnvironmentID     string `gorm:"type:uuid"`
	Domain            string
	LocalPart         string
	Enabled           bool
	WebhookEndpointID *string
	CreatedAt         time.Time
	Route             string
	ForwardTo         string
}

func (InboundAddress) TableName() string { return "inbound_addresses" }

type InboundSpool struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	Kind           string
	EnvelopeFrom   string
	EnvelopeTo     string `gorm:"type:text[]"`
	Path           string
	Size           int
	State          string
	Attempts       int
	LeaseExpiresAt *time.Time
	ErrorCode      string
	ReceivedAt     time.Time
}

func (InboundSpool) TableName() string { return "inbound_spool" }

type InboundMessage struct {
	ID                 string `gorm:"type:uuid;primaryKey"`
	ProjectID          string `gorm:"type:uuid"`
	EnvironmentID      string `gorm:"type:uuid"`
	AddressID          *string
	SpoolID            string
	EnvelopeFrom       string
	EnvelopeTo         string
	HeaderFrom         string
	Subject            string
	TextEnc            []byte
	HTMLEnc            []byte
	Headers            []byte `gorm:"type:jsonb"`
	SpamScore          *float32
	RawSize            int
	ReceivedAt         time.Time
	RetentionExpiresAt time.Time
	BodiesPurgedAt     *time.Time
	InReplyToMessageID *string
	SearchTSV          string `gorm:"column:search_tsv"`
}

func (InboundMessage) TableName() string { return "inbound_messages" }

type InboundAttachment struct {
	ID               string `gorm:"type:uuid;primaryKey"`
	InboundMessageID string
	Filename         string
	ContentType      string
	Size             int
	StoragePath      string
	SHA256           string
}

func (InboundAttachment) TableName() string { return "inbound_attachments" }

type Bounce struct {
	ID              string `gorm:"type:uuid;primaryKey"`
	MessageID       *string
	ProjectID       *string
	EnvironmentID   *string
	RecipientHash   []byte
	RecipientMasked string
	Type            string
	StatusCode      string
	Diagnostic      string
	DedupeHash      []byte
	ReceivedAt      time.Time
}

func (Bounce) TableName() string { return "bounces" }

type DNSCheck struct {
	ID         int64 `gorm:"primaryKey"`
	Domain     string
	RecordType string
	Expected   string
	Observed   string
	Pass       bool
	CheckedAt  time.Time
}

func (DNSCheck) TableName() string { return "dns_checks" }

type MessageAttachment struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	MessageID   string
	Filename    string
	ContentType string
	Size        int
	DataEnc     []byte
}

func (MessageAttachment) TableName() string { return "message_attachments" }

type Domain struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	ProjectID   string `gorm:"type:uuid"`
	Name        string
	Purpose     string
	VerifyToken string
	VerifiedAt  *time.Time
	SPFOk       bool `gorm:"column:spf_ok"`
	DKIMOk      bool `gorm:"column:dkim_ok"`
	DMARCOk     bool `gorm:"column:dmarc_ok"`
	CreatedAt   time.Time
}

func (Domain) TableName() string { return "domains" }

type PasswordReset struct {
	TokenHash []byte `gorm:"primaryKey"`
	UserID    string
	ExpiresAt time.Time
}

func (PasswordReset) TableName() string { return "password_resets" }

type AlertChannel struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	Kind      string
	Target    string
	Enabled   bool
	CreatedAt time.Time
}

func (AlertChannel) TableName() string { return "alert_channels" }
