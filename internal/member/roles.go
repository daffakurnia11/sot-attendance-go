package member

import (
	"context"
	"fmt"
)

type Role struct {
	DiscordUserID string
	ID            string
	Name          string
}

// SyncRoles reconciles existing accounts from a successfully fetched full guild roster.
// Missing membership or target roles clears stale values; it never creates accounts.
func (r *Repository) SyncRoles(ctx context.Context, roles []Role) error {
	ids, roleIDs, names := []string{}, []string{}, []string{}
	for _, role := range roles {
		ids = append(ids, role.DiscordUserID)
		roleIDs = append(roleIDs, role.ID)
		names = append(names, role.Name)
	}
	_, err := r.database.Exec(ctx, `WITH roles AS (
 SELECT * FROM unnest($1::text[],$2::text[],$3::text[]) AS data(discord_user_id,role_id,role_name)
 ), desired AS (
 SELECT m.id,NULLIF(r.role_id,'') AS role_id,NULLIF(r.role_name,'') AS role_name FROM members m LEFT JOIN roles r ON r.discord_user_id=m.discord_user_id
 ) UPDATE members m SET role_id=d.role_id,role_name=d.role_name FROM desired d
 WHERE m.id=d.id AND (m.role_id IS DISTINCT FROM d.role_id OR m.role_name IS DISTINCT FROM d.role_name)`, ids, roleIDs, names)
	if err != nil {
		return fmt.Errorf("sync member roles: %w", err)
	}
	return nil
}
