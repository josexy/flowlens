package apicollectionservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"

	"github.com/google/uuid"
)

const apiCollectionPayloadVersion = 1

type collectionRepository struct {
	db *sql.DB
}

type apiNodeRow struct {
	ID             string
	ParentID       sql.NullString
	Kind           APICollectionNodeType
	Name           string
	NormalizedName string
	SortOrder      int
	CreatedAt      int64
	UpdatedAt      int64
	HTTPMethod     string
}

func newCollectionRepository(db *sql.DB) *collectionRepository {
	return &collectionRepository{db: db}
}

func insertNodeTx(ctx context.Context, tx *sql.Tx, node apiNodeRow) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO api_nodes(
			id, parent_id, kind, name, normalized_name, sort_order,
			created_at, updated_at, http_method
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, node.ID, nullableStringValue(node.ParentID), node.Kind, node.Name, node.NormalizedName,
		node.SortOrder, node.CreatedAt, node.UpdatedAt, node.HTTPMethod); err != nil {
		return fmt.Errorf("insert api collection node %q: %w", node.ID, err)
	}
	return nil
}

func (r *collectionRepository) listCollection(ctx context.Context) (*APICollectionTree, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, parent_id, kind, name, normalized_name, sort_order,
		       created_at, updated_at, http_method
		FROM api_nodes
	`)
	if err != nil {
		return nil, fmt.Errorf("query api collection nodes: %w", err)
	}
	defer rows.Close()

	nodes := make([]apiNodeRow, 0)
	for rows.Next() {
		var node apiNodeRow
		if err := rows.Scan(&node.ID, &node.ParentID, &node.Kind, &node.Name, &node.NormalizedName,
			&node.SortOrder, &node.CreatedAt, &node.UpdatedAt, &node.HTTPMethod); err != nil {
			return nil, fmt.Errorf("scan api collection node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api collection nodes: %w", err)
	}

	tree := newEmptyCollectionStore()
	folders := make(map[string]*APICollectionFolder)
	for _, node := range nodes {
		if node.Kind != APICollectionNodeTypeFolder {
			continue
		}
		folders[node.ID] = &APICollectionFolder{
			ID: node.ID, Name: node.Name, SortOrder: node.SortOrder,
			CreatedAt: node.CreatedAt, UpdatedAt: node.UpdatedAt,
			Folders: []*APICollectionFolder{}, Requests: []*APICollectionRequest{},
		}
	}
	for _, node := range nodes {
		switch node.Kind {
		case APICollectionNodeTypeFolder:
			folder := folders[node.ID]
			if !node.ParentID.Valid {
				tree.Folders = append(tree.Folders, folder)
				continue
			}
			parent := folders[node.ParentID.String]
			if parent == nil {
				return nil, fmt.Errorf("api collection folder %q has missing parent %q", node.ID, node.ParentID.String)
			}
			parent.Folders = append(parent.Folders, folder)
		case APICollectionNodeTypeHTTP, APICollectionNodeTypeWebSocket:
			if !node.ParentID.Valid || folders[node.ParentID.String] == nil {
				return nil, fmt.Errorf("api collection request %q has missing parent", node.ID)
			}
			request := &APICollectionRequest{
				ID: node.ID, Type: node.Kind, Name: node.Name, SortOrder: node.SortOrder,
				CreatedAt: node.CreatedAt, UpdatedAt: node.UpdatedAt,
			}
			if node.Kind == APICollectionNodeTypeHTTP {
				request.HTTP = &SavedHTTPRequest{Method: node.HTTPMethod}
			}
			folders[node.ParentID.String].Requests = append(folders[node.ParentID.String].Requests, request)
		default:
			return nil, fmt.Errorf("unsupported api collection node type %q", node.Kind)
		}
	}
	sortFoldersInPlace(tree.Folders)
	return tree, nil
}

func (r *collectionRepository) getRequest(ctx context.Context, id string) (*APICollectionRequest, error) {
	var node apiNodeRow
	var payloadVersion int
	var payload string
	err := r.db.QueryRowContext(ctx, `
		SELECT n.id, n.parent_id, n.kind, n.name, n.normalized_name, n.sort_order,
		       n.created_at, n.updated_at, n.http_method,
		       p.payload_version, p.payload_json
		FROM api_nodes n
		JOIN api_request_payloads p ON p.node_id = n.id
		WHERE n.id = ?
	`, strings.TrimSpace(id)).Scan(
		&node.ID, &node.ParentID, &node.Kind, &node.Name, &node.NormalizedName, &node.SortOrder,
		&node.CreatedAt, &node.UpdatedAt, &node.HTTPMethod, &payloadVersion, &payload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("request %q not found", strings.TrimSpace(id))
	}
	if err != nil {
		return nil, fmt.Errorf("query api collection request %q: %w", id, err)
	}
	if payloadVersion != apiCollectionPayloadVersion {
		return nil, fmt.Errorf("unsupported api request payload version %d", payloadVersion)
	}
	request, err := requestFromPayload(node.Kind, payload)
	if err != nil {
		return nil, fmt.Errorf("decode api request %q: %w", id, err)
	}
	request.ID = node.ID
	request.ParentID = node.ParentID.String
	request.Name = node.Name
	request.SortOrder = node.SortOrder
	request.CreatedAt = node.CreatedAt
	request.UpdatedAt = node.UpdatedAt
	return request, nil
}

func (r *collectionRepository) referencedManagedFilePaths(ctx context.Context) (map[string]struct{}, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT n.kind, p.payload_json
		FROM api_nodes n
		JOIN api_request_payloads p ON p.node_id = n.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query api request file references: %w", err)
	}
	defer rows.Close()
	paths := make(map[string]struct{})
	for rows.Next() {
		var kind APICollectionNodeType
		var payload string
		if err := rows.Scan(&kind, &payload); err != nil {
			return nil, fmt.Errorf("scan api request file reference: %w", err)
		}
		request, err := requestFromPayload(kind, payload)
		if err != nil {
			return nil, err
		}
		collectCollectionRequestManagedFilePaths(request, paths)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api request file references: %w", err)
	}
	return paths, nil
}

func (r *collectionRepository) createFolder(ctx context.Context, parentID, name string) (*APICollectionFolder, error) {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return nil, errors.New("name cannot be empty")
	}
	parentID = strings.TrimSpace(parentID)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create api folder: %w", err)
	}
	defer tx.Rollback()
	if parentID != "" {
		if err := requireFolderTx(ctx, tx, parentID); err != nil {
			return nil, err
		}
	}
	if exists, err := childNameExistsTx(ctx, tx, parentID, "", trimmedName); err != nil {
		return nil, err
	} else if exists {
		return nil, fmt.Errorf("duplicate node name %q under the same parent", trimmedName)
	}
	sortOrder, err := nextSortOrderTx(ctx, tx, parentID, true)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	folder := &APICollectionFolder{
		ID: uuid.NewString(), Name: trimmedName, SortOrder: sortOrder,
		CreatedAt: now, UpdatedAt: now,
		Folders: []*APICollectionFolder{}, Requests: []*APICollectionRequest{},
	}
	if err := insertNodeTx(ctx, tx, apiNodeRow{
		ID: folder.ID, ParentID: nullableParentID(parentID), Kind: APICollectionNodeTypeFolder,
		Name: folder.Name, NormalizedName: normalizeNodeName(folder.Name), SortOrder: sortOrder,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create api folder: %w", err)
	}
	return folder, nil
}

func (r *collectionRepository) renameNode(ctx context.Context, id, name string) (*APICollectionEntry, error) {
	id = strings.TrimSpace(id)
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return nil, errors.New("name cannot be empty")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin rename api node: %w", err)
	}
	defer tx.Rollback()
	var parentID sql.NullString
	var kind APICollectionNodeType
	var sortOrder int
	var createdAt int64
	if err := tx.QueryRowContext(ctx, `
		SELECT parent_id, kind, sort_order, created_at FROM api_nodes WHERE id = ?
	`, id).Scan(&parentID, &kind, &sortOrder, &createdAt); errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("node %q not found", id)
	} else if err != nil {
		return nil, fmt.Errorf("query api node %q: %w", id, err)
	}
	if exists, err := childNameExistsTx(ctx, tx, parentID.String, id, trimmedName); err != nil {
		return nil, err
	} else if exists {
		return nil, fmt.Errorf("duplicate node name %q under the same parent", trimmedName)
	}
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `
		UPDATE api_nodes SET name = ?, normalized_name = ?, updated_at = ? WHERE id = ?
	`, trimmedName, normalizeNodeName(trimmedName), now, id); err != nil {
		return nil, fmt.Errorf("rename api node %q: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit rename api node %q: %w", id, err)
	}
	if kind == APICollectionNodeTypeFolder {
		return &APICollectionEntry{Folder: &APICollectionFolder{
			ID: id, Name: trimmedName, SortOrder: sortOrder, CreatedAt: createdAt, UpdatedAt: now,
			Folders: []*APICollectionFolder{}, Requests: []*APICollectionRequest{},
		}}, nil
	}
	request, err := r.getRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	return &APICollectionEntry{Request: request}, nil
}

func (r *collectionRepository) moveNode(ctx context.Context, id, newParentID string) (bool, error) {
	id = strings.TrimSpace(id)
	newParentID = strings.TrimSpace(newParentID)
	if id == "" {
		return false, errors.New("node id cannot be empty")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin move api node: %w", err)
	}
	defer tx.Rollback()

	var currentParentID sql.NullString
	var kind APICollectionNodeType
	var name string
	if err := tx.QueryRowContext(ctx, `
		SELECT parent_id, kind, name FROM api_nodes WHERE id = ?
	`, id).Scan(&currentParentID, &kind, &name); errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("node %q not found", id)
	} else if err != nil {
		return false, fmt.Errorf("query api node %q: %w", id, err)
	}

	previousParentID := ""
	if currentParentID.Valid {
		previousParentID = currentParentID.String
	}
	if previousParentID == newParentID {
		return false, nil
	}

	if newParentID == "" {
		if kind != APICollectionNodeTypeFolder {
			return false, errors.New("request nodes must have a parent folder")
		}
	} else {
		if err := requireFolderTx(ctx, tx, newParentID); err != nil {
			return false, err
		}
	}

	if kind == APICollectionNodeTypeFolder && newParentID != "" {
		contains, err := nodeHasAncestorTx(ctx, tx, newParentID, id)
		if err != nil {
			return false, err
		}
		if contains {
			return false, fmt.Errorf("cannot move folder %q into itself or one of its descendants", id)
		}
	}

	if exists, err := childNameExistsTx(ctx, tx, newParentID, id, name); err != nil {
		return false, err
	} else if exists {
		return false, fmt.Errorf("duplicate node name %q under the same parent", name)
	}

	sortOrder, err := nextSortOrderTx(ctx, tx, newParentID, kind == APICollectionNodeTypeFolder)
	if err != nil {
		return false, err
	}
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `
		UPDATE api_nodes
		SET parent_id = ?, sort_order = ?, updated_at = ?
		WHERE id = ?
	`, nullableStringValue(nullableParentID(newParentID)), sortOrder, now, id); err != nil {
		return false, fmt.Errorf("move api node %q: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit move api node %q: %w", id, err)
	}
	return true, nil
}

func (r *collectionRepository) deleteNodes(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, errors.New("node ids cannot be empty")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin delete api nodes: %w", err)
	}
	defer tx.Rollback()
	for _, id := range ids {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM api_nodes WHERE id = ?)`, id).Scan(&exists); err != nil {
			return nil, fmt.Errorf("query api node %q: %w", id, err)
		}
		if exists == 0 {
			return nil, fmt.Errorf("node %q not found", id)
		}
	}

	query, args := recursiveSelectedNodesQuery(ids, `
		SELECT n.kind, p.payload_json
		FROM selected s
		JOIN api_nodes n ON n.id = s.id
		JOIN api_request_payloads p ON p.node_id = n.id
	`)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query deleted api request payloads: %w", err)
	}
	deletedFiles := make(map[string]struct{})
	for rows.Next() {
		var kind APICollectionNodeType
		var payload string
		if err := rows.Scan(&kind, &payload); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan deleted api request payload: %w", err)
		}
		request, err := requestFromPayload(kind, payload)
		if err != nil {
			rows.Close()
			return nil, err
		}
		collectCollectionRequestManagedFilePaths(request, deletedFiles)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close deleted api request payload rows: %w", err)
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM api_nodes WHERE id = ?`, id); err != nil {
			return nil, fmt.Errorf("delete api node %q: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit delete api nodes: %w", err)
	}
	return mapKeys(deletedFiles), nil
}

func (r *collectionRepository) saveHTTPRequest(ctx context.Context, parentID, name string, request *SavedHTTPRequest) (*APICollectionRequest, error) {
	return r.saveRequest(ctx, parentID, name, APICollectionNodeTypeHTTP, request, nil)
}

func (r *collectionRepository) saveWebSocketRequest(ctx context.Context, parentID, name string, request *SavedWebSocketRequest) (*APICollectionRequest, error) {
	return r.saveRequest(ctx, parentID, name, APICollectionNodeTypeWebSocket, nil, request)
}

func (r *collectionRepository) saveRequest(ctx context.Context, parentID, name string, kind APICollectionNodeType, httpRequest *SavedHTTPRequest, wsRequest *SavedWebSocketRequest) (*APICollectionRequest, error) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return nil, errors.New("parent folder id cannot be empty")
	}
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return nil, errors.New("name cannot be empty")
	}
	payload, err := marshalRequestPayload(kind, httpRequest, wsRequest)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin save api request: %w", err)
	}
	defer tx.Rollback()
	if err := requireFolderTx(ctx, tx, parentID); err != nil {
		return nil, err
	}
	uniqueName, err := nextUniqueChildNameTx(ctx, tx, parentID, trimmedName)
	if err != nil {
		return nil, err
	}
	sortOrder, err := nextSortOrderTx(ctx, tx, parentID, false)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	id := uuid.NewString()
	method := ""
	if httpRequest != nil {
		method = httpRequest.Method
	}
	if err := insertNodeTx(ctx, tx, apiNodeRow{
		ID: id, ParentID: nullableParentID(parentID), Kind: kind,
		Name: uniqueName, NormalizedName: normalizeNodeName(uniqueName), SortOrder: sortOrder,
		CreatedAt: now, UpdatedAt: now, HTTPMethod: method,
	}); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO api_request_payloads(node_id, payload_version, payload_json)
		VALUES (?, ?, ?)
	`, id, apiCollectionPayloadVersion, payload); err != nil {
		return nil, fmt.Errorf("insert api request payload %q: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit save api request: %w", err)
	}
	return r.getRequest(ctx, id)
}

func (r *collectionRepository) updateHTTPRequest(ctx context.Context, id string, request *SavedHTTPRequest) (*APICollectionRequest, error) {
	return r.updateRequest(ctx, id, APICollectionNodeTypeHTTP, request, nil)
}

func (r *collectionRepository) updateWebSocketRequest(ctx context.Context, id string, request *SavedWebSocketRequest) (*APICollectionRequest, error) {
	return r.updateRequest(ctx, id, APICollectionNodeTypeWebSocket, nil, request)
}

func (r *collectionRepository) updateRequest(ctx context.Context, id string, kind APICollectionNodeType, httpRequest *SavedHTTPRequest, wsRequest *SavedWebSocketRequest) (*APICollectionRequest, error) {
	id = strings.TrimSpace(id)
	payload, err := marshalRequestPayload(kind, httpRequest, wsRequest)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin update api request: %w", err)
	}
	defer tx.Rollback()
	var currentKind APICollectionNodeType
	if err := tx.QueryRowContext(ctx, `SELECT kind FROM api_nodes WHERE id = ?`, id).Scan(&currentKind); errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("node %q not found", id)
	} else if err != nil {
		return nil, fmt.Errorf("query api node %q: %w", id, err)
	}
	if currentKind != kind {
		return nil, fmt.Errorf("node %q is not an %s api", id, kind)
	}
	method := ""
	if httpRequest != nil {
		method = httpRequest.Method
	}
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `UPDATE api_nodes SET updated_at = ?, http_method = ? WHERE id = ?`, now, method, id); err != nil {
		return nil, fmt.Errorf("update api node %q: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE api_request_payloads SET payload_version = ?, payload_json = ? WHERE node_id = ?
	`, apiCollectionPayloadVersion, payload, id); err != nil {
		return nil, fmt.Errorf("update api request payload %q: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update api request: %w", err)
	}
	return r.getRequest(ctx, id)
}

func marshalRequestPayload(kind APICollectionNodeType, httpRequest *SavedHTTPRequest, wsRequest *SavedWebSocketRequest) ([]byte, error) {
	var value any
	switch kind {
	case APICollectionNodeTypeHTTP:
		if httpRequest == nil || wsRequest != nil {
			return nil, errors.New("http api request payload is invalid")
		}
		stored, err := prepareHTTPRequestForStorage(httpRequest)
		if err != nil {
			return nil, fmt.Errorf("prepare http api request files: %w", err)
		}
		value = stored
	case APICollectionNodeTypeWebSocket:
		if wsRequest == nil || httpRequest != nil {
			return nil, errors.New("websocket api request payload is invalid")
		}
		stored, err := prepareWebSocketRequestForStorage(wsRequest)
		if err != nil {
			return nil, fmt.Errorf("prepare websocket api request files: %w", err)
		}
		value = stored
	default:
		return nil, fmt.Errorf("unsupported api node type: %s", kind)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode %s api request payload: %w", kind, err)
	}
	return payload, nil
}

func requestFromPayload(kind APICollectionNodeType, payload string) (*APICollectionRequest, error) {
	request := &APICollectionRequest{Type: kind}
	switch kind {
	case APICollectionNodeTypeHTTP:
		request.HTTP = new(SavedHTTPRequest)
		if err := json.Unmarshal([]byte(payload), request.HTTP); err != nil {
			return nil, fmt.Errorf("decode http api request payload: %w", err)
		}
		if request.HTTP.Protocol == "" {
			request.HTTP.Protocol = proxyservice.SendRequestProtocolAuto
		}
		if request.HTTP.TLSClientHelloID == "" {
			request.HTTP.TLSClientHelloID = proxyservice.TLSClientHelloGolang
		}
		if err := hydrateHTTPRequestFiles(request.HTTP); err != nil {
			return nil, fmt.Errorf("hydrate http api request files: %w", err)
		}
	case APICollectionNodeTypeWebSocket:
		request.WebSocket = new(SavedWebSocketRequest)
		if err := json.Unmarshal([]byte(payload), request.WebSocket); err != nil {
			return nil, fmt.Errorf("decode websocket api request payload: %w", err)
		}
		if request.WebSocket.TLSClientHelloID == "" {
			request.WebSocket.TLSClientHelloID = proxyservice.TLSClientHelloGolang
		}
		if err := hydrateWebSocketRequestFiles(request.WebSocket); err != nil {
			return nil, fmt.Errorf("hydrate websocket api request files: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported api node type: %s", kind)
	}
	return request, nil
}

func requireFolderTx(ctx context.Context, tx *sql.Tx, id string) error {
	var kind APICollectionNodeType
	if err := tx.QueryRowContext(ctx, `SELECT kind FROM api_nodes WHERE id = ?`, id).Scan(&kind); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("folder %q not found", id)
	} else if err != nil {
		return fmt.Errorf("query api folder %q: %w", id, err)
	}
	if kind != APICollectionNodeTypeFolder {
		return fmt.Errorf("node %q is not a folder", id)
	}
	return nil
}

func nodeHasAncestorTx(ctx context.Context, tx *sql.Tx, nodeID, ancestorID string) (bool, error) {
	var exists int
	if err := tx.QueryRowContext(ctx, `
		WITH RECURSIVE ancestors(id, parent_id) AS (
			SELECT id, parent_id FROM api_nodes WHERE id = ?
			UNION
			SELECT parent.id, parent.parent_id
			FROM api_nodes parent
			JOIN ancestors child ON parent.id = child.parent_id
		)
		SELECT EXISTS(SELECT 1 FROM ancestors WHERE id = ?)
	`, nodeID, ancestorID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check api node ancestry: %w", err)
	}
	return exists != 0, nil
}

func childNameExistsTx(ctx context.Context, tx *sql.Tx, parentID, selfID, name string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM api_nodes WHERE parent_id IS NULL AND normalized_name = ? AND id <> ?)`
	args := []any{normalizeNodeName(name), selfID}
	if parentID != "" {
		query = `SELECT EXISTS(SELECT 1 FROM api_nodes WHERE parent_id = ? AND normalized_name = ? AND id <> ?)`
		args = []any{parentID, normalizeNodeName(name), selfID}
	}
	var exists int
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("check api child name: %w", err)
	}
	return exists != 0, nil
}

func nextUniqueChildNameTx(ctx context.Context, tx *sql.Tx, parentID, name string) (string, error) {
	for index := 0; ; index++ {
		candidate := name
		if index > 0 {
			candidate = fmt.Sprintf("%s (%d)", name, index)
		}
		exists, err := childNameExistsTx(ctx, tx, parentID, "", candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
}

func nextSortOrderTx(ctx context.Context, tx *sql.Tx, parentID string, folders bool) (int, error) {
	kindCondition := `kind IN ('http', 'websocket')`
	if folders {
		kindCondition = `kind = 'folder'`
	}
	query := fmt.Sprintf(`SELECT COALESCE(MAX(sort_order), -1) + 1 FROM api_nodes WHERE parent_id IS NULL AND %s`, kindCondition)
	args := []any{}
	if parentID != "" {
		query = fmt.Sprintf(`SELECT COALESCE(MAX(sort_order), -1) + 1 FROM api_nodes WHERE parent_id = ? AND %s`, kindCondition)
		args = append(args, parentID)
	}
	var order int
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&order); err != nil {
		return 0, fmt.Errorf("query next api sort order: %w", err)
	}
	return order, nil
}

func recursiveSelectedNodesQuery(ids []string, selectSQL string) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for index, id := range ids {
		placeholders[index] = "?"
		args[index] = id
	}
	query := fmt.Sprintf(`
		WITH RECURSIVE selected(id) AS (
			SELECT id FROM api_nodes WHERE id IN (%s)
			UNION
			SELECT child.id FROM api_nodes child JOIN selected parent ON child.parent_id = parent.id
		)
		%s
	`, strings.Join(placeholders, ","), selectSQL)
	return query, args
}

func nullableParentID(parentID string) sql.NullString {
	parentID = strings.TrimSpace(parentID)
	return sql.NullString{String: parentID, Valid: parentID != ""}
}

func nullableStringValue(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
