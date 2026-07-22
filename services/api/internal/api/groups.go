package api

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/api/internal/store"
)

const maxGroupMembers = 100

type createGroupRequest struct {
	MemberUsernames []string `json:"member_usernames"`
}

type groupMembersRequest struct {
	MemberUsernames []string `json:"member_usernames"`
}

type groupOwnershipRequest struct {
	Username string `json:"username"`
}

type sendGroupMessageRequest struct {
	Revision  uint64                   `json:"revision"`
	Envelopes []messageEnvelopeRequest `json:"envelopes"`
}

type groupResponse struct {
	ID            string                `json:"id"`
	OwnerUsername string                `json:"owner_username"`
	Revision      uint64                `json:"revision"`
	CreatedAt     time.Time             `json:"created_at"`
	Members       []groupMemberResponse `json:"members"`
}

type groupMemberResponse struct {
	Username string    `json:"username"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

type groupDevicesResponse struct {
	Revision uint64                `json:"revision"`
	Devices  []groupDeviceResponse `json:"devices"`
}

type groupDeviceResponse struct {
	Username string `json:"username"`
	UserID   string `json:"user_id"`
	DeviceID string `json:"device_id"`
}

func (server *Server) createGroup(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	var input createGroupRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	memberIDs, valid := server.resolveGroupUsers(input.MemberUsernames, 1, maxGroupMembers-1)
	if !valid {
		writeError(writer, http.StatusBadRequest, "invalid group members")
		return
	}
	nonOwnerMemberIDs := make([]string, 0, len(memberIDs))
	for _, memberID := range memberIDs {
		if memberID != claims.Subject {
			nonOwnerMemberIDs = append(nonOwnerMemberIDs, memberID)
		}
	}
	if len(nonOwnerMemberIDs) == 0 {
		writeError(writer, http.StatusBadRequest, "group requires another member")
		return
	}
	group, members, err := server.store.CreateGroup(claims.Subject, nonOwnerMemberIDs)
	if server.writeGroupError(writer, err) {
		return
	}
	response, err := server.groupResponse(group, members)
	if server.writeGroupError(writer, err) {
		return
	}
	writeJSON(writer, http.StatusCreated, response)
}

func (server *Server) listGroups(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	groups, err := server.store.Groups(claims.Subject)
	if server.writeGroupError(writer, err) {
		return
	}
	responses := make([]groupResponse, 0, len(groups))
	for _, group := range groups {
		current, members, err := server.store.GroupMembers(group.ID, claims.Subject)
		if server.writeGroupError(writer, err) {
			return
		}
		response, err := server.groupResponse(current, members)
		if server.writeGroupError(writer, err) {
			return
		}
		responses = append(responses, response)
	}
	writeJSON(writer, http.StatusOK, responses)
}

func (server *Server) getGroup(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	groupID, valid := groupID(request.URL.Path, "")
	if !valid {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	group, members, err := server.store.GroupMembers(groupID, claims.Subject)
	if server.writeGroupError(writer, err) {
		return
	}
	response, err := server.groupResponse(group, members)
	if server.writeGroupError(writer, err) {
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

func (server *Server) addGroupMembers(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	groupID, valid := groupID(request.URL.Path, "/members")
	if !valid {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	var input groupMembersRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	memberIDs, valid := server.resolveGroupUsers(input.MemberUsernames, 1, maxGroupMembers-1)
	if !valid {
		writeError(writer, http.StatusBadRequest, "invalid group members")
		return
	}
	group, members, err := server.store.AddGroupMembers(groupID, claims.Subject, memberIDs)
	if server.writeGroupError(writer, err) {
		return
	}
	response, err := server.groupResponse(group, members)
	if server.writeGroupError(writer, err) {
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

func (server *Server) removeGroupMember(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	groupID, username, valid := groupMemberPath(request.URL.Path)
	if !valid || !validUsername(username) {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	user, err := server.store.FindUserByUsername(username)
	if errors.Is(err, store.ErrUserNotFound) {
		writeError(writer, http.StatusNotFound, "group member not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "group update failed")
		return
	}
	group, members, err := server.store.RemoveGroupMember(groupID, claims.Subject, user.ID)
	if server.writeGroupError(writer, err) {
		return
	}
	response, err := server.groupResponse(group, members)
	if server.writeGroupError(writer, err) {
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

func (server *Server) transferGroupOwnership(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	groupID, valid := groupID(request.URL.Path, "/owner")
	if !valid {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	var input groupOwnershipRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !validUsername(input.Username) {
		writeError(writer, http.StatusBadRequest, "invalid group owner")
		return
	}
	user, err := server.store.FindUserByUsername(input.Username)
	if errors.Is(err, store.ErrUserNotFound) {
		writeError(writer, http.StatusNotFound, "group member not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "group update failed")
		return
	}
	group, members, err := server.store.TransferGroupOwnership(groupID, claims.Subject, user.ID)
	if server.writeGroupError(writer, err) {
		return
	}
	response, err := server.groupResponse(group, members)
	if server.writeGroupError(writer, err) {
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

func (server *Server) groupDevices(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	groupID, valid := groupID(request.URL.Path, "/devices")
	if !valid {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	group, members, err := server.store.GroupMembers(groupID, claims.Subject)
	if server.writeGroupError(writer, err) {
		return
	}
	devices := make([]groupDeviceResponse, 0)
	for _, member := range members {
		user, err := server.store.FindUserByID(member.UserID)
		if server.writeGroupError(writer, err) {
			return
		}
		activeDevices, err := server.store.ActiveDevices(member.UserID)
		if server.writeGroupError(writer, err) {
			return
		}
		for _, device := range activeDevices {
			if device.ID != claims.DeviceID {
				devices = append(devices, groupDeviceResponse{Username: user.Username, UserID: user.ID, DeviceID: device.ID})
			}
		}
	}
	sort.Slice(devices, func(left int, right int) bool {
		if devices[left].Username == devices[right].Username {
			return devices[left].DeviceID < devices[right].DeviceID
		}
		return devices[left].Username < devices[right].Username
	})
	writeJSON(writer, http.StatusOK, groupDevicesResponse{Revision: group.Revision, Devices: devices})
}

func (server *Server) sendGroupMessage(writer http.ResponseWriter, request *http.Request) {
	claims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.allowRequest(writer, request, "group-message", claims.DeviceID, 120, time.Minute) {
		return
	}
	groupID, valid := groupID(request.URL.Path, "/messages")
	if !valid {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	var input sendGroupMessageRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if input.Revision == 0 || len(input.Envelopes) > maxOneTimePreKeys {
		writeError(writer, http.StatusBadRequest, "invalid group message")
		return
	}
	envelopes := make([]store.MessageEnvelope, 0, len(input.Envelopes))
	for _, envelope := range input.Envelopes {
		ciphertext, valid := decodeCiphertext(envelope.Ciphertext)
		if !valid || envelope.RecipientDeviceID == "" || strings.Contains(envelope.RecipientDeviceID, "/") {
			writeError(writer, http.StatusBadRequest, "invalid group message")
			return
		}
		envelopes = append(envelopes, store.MessageEnvelope{
			RecipientDeviceID: envelope.RecipientDeviceID,
			Ciphertext:        ciphertext,
		})
	}
	messages, err := server.store.FanoutGroupMessage(
		groupID,
		input.Revision,
		claims.Subject,
		claims.DeviceID,
		envelopes,
	)
	if server.writeGroupError(writer, err) {
		return
	}
	responses := make([]messageResponse, 0, len(messages))
	for _, message := range messages {
		response, err := server.messageResponse(message)
		if server.writeGroupError(writer, err) {
			return
		}
		responses = append(responses, response)
		published := server.hub.publish(message.RecipientDeviceID, websocketEvent{Type: "message", Message: &response})
		if !published && server.push != nil {
			_ = server.push.MessageAvailable(request.Context(), message.RecipientUserID, message.RecipientDeviceID)
		}
	}
	writeJSON(writer, http.StatusAccepted, sendMessageResponse{Messages: responses})
}

func (server *Server) resolveGroupUsers(usernames []string, minimum int, maximum int) ([]string, bool) {
	if len(usernames) < minimum || len(usernames) > maximum {
		return nil, false
	}
	identifiers := make([]string, 0, len(usernames))
	seen := make(map[string]struct{}, len(usernames))
	for _, username := range usernames {
		if !validUsername(username) {
			return nil, false
		}
		key := strings.ToLower(username)
		if _, exists := seen[key]; exists {
			return nil, false
		}
		seen[key] = struct{}{}
		user, err := server.store.FindUserByUsername(username)
		if err != nil {
			return nil, false
		}
		identifiers = append(identifiers, user.ID)
	}
	return identifiers, true
}

func (server *Server) groupResponse(group store.Group, members []store.GroupMember) (groupResponse, error) {
	owner, err := server.store.FindUserByID(group.OwnerID)
	if err != nil {
		return groupResponse{}, err
	}
	memberResponses := make([]groupMemberResponse, 0, len(members))
	for _, member := range members {
		user, err := server.store.FindUserByID(member.UserID)
		if err != nil {
			return groupResponse{}, err
		}
		memberResponses = append(memberResponses, groupMemberResponse{
			Username: user.Username,
			Role:     member.Role,
			JoinedAt: member.JoinedAt,
		})
	}
	return groupResponse{
		ID:            group.ID,
		OwnerUsername: owner.Username,
		Revision:      group.Revision,
		CreatedAt:     group.CreatedAt,
		Members:       memberResponses,
	}, nil
}

func (server *Server) writeGroupError(writer http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, store.ErrGroupNotFound):
		writeError(writer, http.StatusNotFound, "group not found")
	case errors.Is(err, store.ErrGroupForbidden):
		writeError(writer, http.StatusForbidden, "group action is forbidden")
	case errors.Is(err, store.ErrGroupStateChanged), errors.Is(err, store.ErrDeviceSetChanged):
		writeError(writer, http.StatusConflict, "group membership changed")
	case errors.Is(err, store.ErrGroupMemberExists):
		writeError(writer, http.StatusConflict, "group member already exists")
	case errors.Is(err, store.ErrUserNotFound):
		writeError(writer, http.StatusNotFound, "user not found")
	case errors.Is(err, store.ErrDeviceNotFound), errors.Is(err, store.ErrDeviceRevoked):
		writeError(writer, http.StatusUnauthorized, "authentication required")
	default:
		writeError(writer, http.StatusInternalServerError, "group operation failed")
	}
	return true
}

func groupID(path string, suffix string) (string, bool) {
	value := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/groups/"), suffix)
	return value, value != "" && !strings.Contains(value, "/")
}

func groupMemberPath(path string) (string, string, bool) {
	value := strings.TrimPrefix(path, "/v1/groups/")
	parts := strings.Split(value, "/members/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[1], "/") {
		return "", "", false
	}
	return parts[0], parts[1], !strings.Contains(parts[0], "/")
}
