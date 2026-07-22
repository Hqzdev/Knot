package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type groupQueryExecutor interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (store *PostgresStore) CreateGroup(ownerUserID string, memberUserIDs []string) (Group, []GroupMember, error) {
	memberIDs, err := normalizedGroupMemberIDs(ownerUserID, memberUserIDs)
	if err != nil {
		return Group{}, nil, err
	}
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return Group{}, nil, err
	}
	defer transaction.Rollback(requestContext)
	for _, memberID := range memberIDs {
		var found string
		err := transaction.QueryRow(
			requestContext,
			`SELECT id FROM users WHERE id = $1`,
			memberID,
		).Scan(&found)
		if errors.Is(err, pgx.ErrNoRows) {
			return Group{}, nil, ErrUserNotFound
		}
		if err != nil {
			return Group{}, nil, err
		}
	}
	now := time.Now().UTC()
	group := Group{ID: randomID(), OwnerID: ownerUserID, Revision: 1, CreatedAt: now}
	_, err = transaction.Exec(
		requestContext,
		`INSERT INTO message_groups (id, owner_user_id, revision, created_at) VALUES ($1, $2, $3, $4)`,
		group.ID,
		group.OwnerID,
		int64(group.Revision),
		group.CreatedAt,
	)
	if err != nil {
		return Group{}, nil, err
	}
	for _, memberID := range memberIDs {
		role := "member"
		if memberID == ownerUserID {
			role = "owner"
		}
		_, err := transaction.Exec(
			requestContext,
			`INSERT INTO group_members (group_id, user_id, role, joined_at) VALUES ($1, $2, $3, $4)`,
			group.ID,
			memberID,
			role,
			now,
		)
		if err != nil {
			return Group{}, nil, err
		}
	}
	members, err := queryGroupMembers(requestContext, transaction, group.ID)
	if err != nil {
		return Group{}, nil, err
	}
	if err := transaction.Commit(requestContext); err != nil {
		return Group{}, nil, err
	}
	return group, members, nil
}

func (store *PostgresStore) Groups(userID string) ([]Group, error) {
	rows, err := store.pool.Query(
		context.Background(),
		`SELECT g.id, g.owner_user_id, g.revision, g.created_at
		 FROM message_groups g
		 JOIN group_members gm ON gm.group_id = g.id
		 WHERE gm.user_id = $1
		 ORDER BY g.created_at, g.id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]Group, 0)
	for rows.Next() {
		group, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (store *PostgresStore) GroupMembers(groupID string, requestingUserID string) (Group, []GroupMember, error) {
	requestContext := context.Background()
	group, err := queryGroup(requestContext, store.pool, groupID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, nil, ErrGroupNotFound
	}
	if err != nil {
		return Group{}, nil, err
	}
	if err := requireGroupMembership(requestContext, store.pool, groupID, requestingUserID, false); err != nil {
		return Group{}, nil, err
	}
	members, err := queryGroupMembers(requestContext, store.pool, groupID)
	return group, members, err
}

func (store *PostgresStore) AddGroupMembers(groupID string, requestingUserID string, memberUserIDs []string) (Group, []GroupMember, error) {
	if len(memberUserIDs) == 0 {
		return Group{}, nil, ErrGroupStateChanged
	}
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return Group{}, nil, err
	}
	defer transaction.Rollback(requestContext)
	group, err := lockedOwnedGroup(requestContext, transaction, groupID, requestingUserID)
	if err != nil {
		return Group{}, nil, err
	}
	var memberCount int
	if err := transaction.QueryRow(
		requestContext,
		`SELECT COUNT(*) FROM group_members WHERE group_id = $1`,
		groupID,
	).Scan(&memberCount); err != nil {
		return Group{}, nil, err
	}
	if memberCount+len(memberUserIDs) > maxGroupMembers {
		return Group{}, nil, ErrGroupStateChanged
	}
	seen := make(map[string]struct{}, len(memberUserIDs))
	now := time.Now().UTC()
	for _, memberID := range memberUserIDs {
		if _, exists := seen[memberID]; exists {
			return Group{}, nil, ErrGroupMemberExists
		}
		seen[memberID] = struct{}{}
		var found string
		err := transaction.QueryRow(
			requestContext,
			`SELECT id FROM users WHERE id = $1`,
			memberID,
		).Scan(&found)
		if errors.Is(err, pgx.ErrNoRows) {
			return Group{}, nil, ErrUserNotFound
		}
		if err != nil {
			return Group{}, nil, err
		}
		_, err = transaction.Exec(
			requestContext,
			`INSERT INTO group_members (group_id, user_id, role, joined_at) VALUES ($1, $2, 'member', $3)`,
			groupID,
			memberID,
			now,
		)
		if isUniqueViolation(err) {
			return Group{}, nil, ErrGroupMemberExists
		}
		if err != nil {
			return Group{}, nil, err
		}
	}
	group, err = incrementGroupRevision(requestContext, transaction, group)
	if err != nil {
		return Group{}, nil, err
	}
	members, err := queryGroupMembers(requestContext, transaction, groupID)
	if err != nil {
		return Group{}, nil, err
	}
	if err := transaction.Commit(requestContext); err != nil {
		return Group{}, nil, err
	}
	return group, members, nil
}

func (store *PostgresStore) RemoveGroupMember(groupID string, requestingUserID string, memberUserID string) (Group, []GroupMember, error) {
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return Group{}, nil, err
	}
	defer transaction.Rollback(requestContext)
	group, err := queryGroup(requestContext, transaction, groupID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, nil, ErrGroupNotFound
	}
	if err != nil {
		return Group{}, nil, err
	}
	if err := requireGroupMembership(requestContext, transaction, groupID, requestingUserID, false); err != nil {
		return Group{}, nil, err
	}
	if requestingUserID != group.OwnerID && requestingUserID != memberUserID {
		return Group{}, nil, ErrGroupForbidden
	}
	if memberUserID == group.OwnerID {
		return Group{}, nil, ErrGroupForbidden
	}
	command, err := transaction.Exec(
		requestContext,
		`DELETE FROM group_members WHERE group_id = $1 AND user_id = $2`,
		groupID,
		memberUserID,
	)
	if err != nil {
		return Group{}, nil, err
	}
	if command.RowsAffected() != 1 {
		return Group{}, nil, ErrGroupNotFound
	}
	group, err = incrementGroupRevision(requestContext, transaction, group)
	if err != nil {
		return Group{}, nil, err
	}
	members, err := queryGroupMembers(requestContext, transaction, groupID)
	if err != nil {
		return Group{}, nil, err
	}
	if err := transaction.Commit(requestContext); err != nil {
		return Group{}, nil, err
	}
	return group, members, nil
}

func (store *PostgresStore) TransferGroupOwnership(groupID string, requestingUserID string, memberUserID string) (Group, []GroupMember, error) {
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return Group{}, nil, err
	}
	defer transaction.Rollback(requestContext)
	group, err := lockedOwnedGroup(requestContext, transaction, groupID, requestingUserID)
	if err != nil {
		return Group{}, nil, err
	}
	if memberUserID == group.OwnerID {
		members, err := queryGroupMembers(requestContext, transaction, groupID)
		if err != nil {
			return Group{}, nil, err
		}
		if err := transaction.Commit(requestContext); err != nil {
			return Group{}, nil, err
		}
		return group, members, nil
	}
	if err := requireGroupMembership(requestContext, transaction, groupID, memberUserID, false); err != nil {
		if errors.Is(err, ErrGroupForbidden) {
			return Group{}, nil, ErrGroupNotFound
		}
		return Group{}, nil, err
	}
	if _, err := transaction.Exec(
		requestContext,
		`UPDATE group_members SET role = CASE WHEN user_id = $1 THEN 'owner' ELSE 'member' END
		 WHERE group_id = $2 AND user_id IN ($1, $3)`,
		memberUserID,
		groupID,
		group.OwnerID,
	); err != nil {
		return Group{}, nil, err
	}
	group.OwnerID = memberUserID
	group.Revision++
	_, err = transaction.Exec(
		requestContext,
		`UPDATE message_groups SET owner_user_id = $1, revision = $2 WHERE id = $3`,
		group.OwnerID,
		int64(group.Revision),
		group.ID,
	)
	if err != nil {
		return Group{}, nil, err
	}
	members, err := queryGroupMembers(requestContext, transaction, groupID)
	if err != nil {
		return Group{}, nil, err
	}
	if err := transaction.Commit(requestContext); err != nil {
		return Group{}, nil, err
	}
	return group, members, nil
}

func (store *PostgresStore) FanoutGroupMessage(groupID string, expectedRevision uint64, senderUserID string, senderDeviceID string, envelopes []MessageEnvelope) ([]PendingMessage, error) {
	requestContext := context.Background()
	transaction, err := store.pool.Begin(requestContext)
	if err != nil {
		return nil, err
	}
	defer transaction.Rollback(requestContext)
	group, err := queryGroup(requestContext, transaction, groupID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	if group.Revision != expectedRevision {
		return nil, ErrGroupStateChanged
	}
	if err := requireGroupMembership(requestContext, transaction, groupID, senderUserID, false); err != nil {
		return nil, err
	}
	memberRows, err := transaction.Query(
		requestContext,
		`SELECT user_id FROM group_members WHERE group_id = $1 ORDER BY user_id`,
		groupID,
	)
	if err != nil {
		return nil, err
	}
	memberIDs := make([]string, 0)
	for memberRows.Next() {
		var memberID string
		if err := memberRows.Scan(&memberID); err != nil {
			memberRows.Close()
			return nil, err
		}
		memberIDs = append(memberIDs, memberID)
	}
	if err := memberRows.Err(); err != nil {
		memberRows.Close()
		return nil, err
	}
	memberRows.Close()
	for _, memberID := range memberIDs {
		if err := lockUser(requestContext, transaction, memberID); err != nil {
			return nil, err
		}
	}
	var senderOwnerID string
	var senderRevokedAt *time.Time
	err = transaction.QueryRow(
		requestContext,
		`SELECT user_id, revoked_at FROM devices WHERE id = $1 FOR SHARE`,
		senderDeviceID,
	).Scan(&senderOwnerID, &senderRevokedAt)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && senderOwnerID != senderUserID {
		return nil, ErrDeviceNotFound
	}
	if err != nil {
		return nil, err
	}
	if senderRevokedAt != nil {
		return nil, ErrDeviceRevoked
	}
	rows, err := transaction.Query(
		requestContext,
		`SELECT d.id, d.user_id, d.name, d.platform, d.created_at, d.revoked_at
		 FROM group_members gm
		 JOIN devices d ON d.user_id = gm.user_id
		 WHERE gm.group_id = $1 AND d.revoked_at IS NULL AND d.id <> $2
		 ORDER BY d.created_at, d.id`,
		groupID,
		senderDeviceID,
	)
	if err != nil {
		return nil, err
	}
	targetDevices := make([]Device, 0)
	for rows.Next() {
		var device Device
		if err := rows.Scan(
			&device.ID,
			&device.UserID,
			&device.Name,
			&device.Platform,
			&device.CreatedAt,
			&device.RevokedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		targetDevices = append(targetDevices, device)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	envelopesByDevice, valid := exactEnvelopes(targetDevices, envelopes)
	if !valid {
		return nil, ErrGroupStateChanged
	}
	now := time.Now().UTC()
	messages := make([]PendingMessage, 0, len(targetDevices))
	for _, device := range targetDevices {
		message := PendingMessage{
			ID:                randomID(),
			RecipientUserID:   device.UserID,
			RecipientDeviceID: device.ID,
			SenderUserID:      senderUserID,
			SenderDeviceID:    senderDeviceID,
			Ciphertext:        append([]byte(nil), envelopesByDevice[device.ID].Ciphertext...),
			CreatedAt:         now,
			GroupID:           groupID,
			GroupRevision:     group.Revision,
		}
		_, err := transaction.Exec(
			requestContext,
			`INSERT INTO device_pending_messages
			 (id, recipient_user_id, recipient_device_id, sender_user_id, sender_device_id,
			  ciphertext, created_at, group_id, group_revision)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			message.ID,
			message.RecipientUserID,
			message.RecipientDeviceID,
			message.SenderUserID,
			message.SenderDeviceID,
			message.Ciphertext,
			message.CreatedAt,
			message.GroupID,
			int64(message.GroupRevision),
		)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := transaction.Commit(requestContext); err != nil {
		return nil, err
	}
	return messages, nil
}

func queryGroup(ctx context.Context, executor groupQueryExecutor, groupID string, lock bool) (Group, error) {
	query := `SELECT id, owner_user_id, revision, created_at FROM message_groups WHERE id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanGroup(executor.QueryRow(ctx, query, groupID))
}

func scanGroup(row pgx.Row) (Group, error) {
	var group Group
	var revision int64
	err := row.Scan(&group.ID, &group.OwnerID, &revision, &group.CreatedAt)
	if err == nil {
		group.Revision = uint64(revision)
	}
	return group, err
}

func queryGroupMembers(ctx context.Context, executor groupQueryExecutor, groupID string) ([]GroupMember, error) {
	rows, err := executor.Query(
		ctx,
		`SELECT group_id, user_id, role, joined_at
		 FROM group_members WHERE group_id = $1 ORDER BY joined_at, user_id`,
		groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := make([]GroupMember, 0)
	for rows.Next() {
		var member GroupMember
		if err := rows.Scan(&member.GroupID, &member.UserID, &member.Role, &member.JoinedAt); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func requireGroupMembership(ctx context.Context, executor groupQueryExecutor, groupID string, userID string, ownerOnly bool) error {
	var role string
	err := executor.QueryRow(
		ctx,
		`SELECT role FROM group_members WHERE group_id = $1 AND user_id = $2`,
		groupID,
		userID,
	).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrGroupForbidden
	}
	if err != nil {
		return err
	}
	if ownerOnly && role != "owner" {
		return ErrGroupForbidden
	}
	return nil
}

func lockedOwnedGroup(ctx context.Context, transaction pgx.Tx, groupID string, ownerUserID string) (Group, error) {
	group, err := queryGroup(ctx, transaction, groupID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, ErrGroupNotFound
	}
	if err != nil {
		return Group{}, err
	}
	if group.OwnerID != ownerUserID {
		return Group{}, ErrGroupForbidden
	}
	return group, nil
}

func incrementGroupRevision(ctx context.Context, transaction pgx.Tx, group Group) (Group, error) {
	group.Revision++
	_, err := transaction.Exec(
		ctx,
		`UPDATE message_groups SET revision = $1 WHERE id = $2`,
		int64(group.Revision),
		group.ID,
	)
	return group, err
}
