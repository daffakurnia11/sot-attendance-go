package payslip

// GuildMemberRoles is the stored membership classification used for payouts.
type GuildMemberRoles struct {
	Roles    []string `json:"roles"`
	Excluded bool     `json:"excluded"`
}
