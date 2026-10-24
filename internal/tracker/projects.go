package tracker

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// projectFieldValue keeps the full native value node together with the IDs
// needed to interpret the structural key stored on Item.ProjectFields.
type projectFieldValue struct {
	ProjectID string          `json:"projectId"`
	FieldID   string          `json:"fieldId"`
	Value     json.RawMessage `json:"value"`
}

type projectItemRef struct {
	ProjectID string
	ContentID string
}

type projectValueRef struct {
	ProjectItemID string
	ProjectID     string
	FieldID       string
	Value         json.RawMessage
	AssetSHA256   string
	JSONPointer   string
}

// joinGitHubProjects attaches captured project field values to already
// normalized issue/PR identities. Project discovery remains in Snapshot's
// Discovery map; this join never creates work items from project-only content.
func joinGitHubProjects(snapshot *Snapshot, files map[string][]byte) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot required")
	}
	if snapshot.Source.Provider != "github" {
		return nil
	}

	projectItems := map[string]projectItemRef{}
	for _, page := range snapshot.Pages {
		if page.Request.Kind != "github-project-items" {
			continue
		}
		if page.Status < 200 || page.Status >= 300 {
			continue
		}
		projectID, err := graphVariableID(page.Request.Body)
		if err != nil {
			return fmt.Errorf("invalid project items request variables")
		}
		body, err := projectAsset(page, files)
		if err != nil {
			return err
		}
		if rawString(body, "id") != projectID {
			return fmt.Errorf("GitHub project items response identity mismatch")
		}
		nodes, err := graphNodes(body, "items")
		if err != nil {
			return fmt.Errorf("invalid GitHub project items response")
		}
		for _, nodeRaw := range nodes {
			node, err := DecodeObject(nodeRaw)
			if err != nil {
				return err
			}
			projectItemID := rawString(node, "id")
			if projectItemID == "" {
				return fmt.Errorf("GitHub project item identity missing")
			}
			var content json.RawMessage
			if err := json.Unmarshal(node["content"], &content); err != nil || string(content) == "null" || len(content) == 0 {
				// Drafts and unsupported content are retained in captured discovery,
				// but cannot be joined to a normalized issue or pull request.
				continue
			}
			contentObject, err := DecodeObject(content)
			if err != nil {
				return err
			}
			contentID := rawString(contentObject, "id")
			if contentID == "" {
				continue
			}
			ref := projectItemRef{ProjectID: projectID, ContentID: contentID}
			if previous, exists := projectItems[projectItemID]; exists && previous != ref {
				return fmt.Errorf("GitHub project item has conflicting project/content identity")
			}
			projectItems[projectItemID] = ref
		}
	}

	values := []projectValueRef{}
	for _, page := range snapshot.Pages {
		if page.Request.Kind != "github-item-fields" {
			continue
		}
		if page.Status < 200 || page.Status >= 300 {
			continue
		}
		projectItemID, err := graphVariableID(page.Request.Body)
		if err != nil {
			return fmt.Errorf("invalid project item field request variables")
		}
		association, ok := projectItems[projectItemID]
		if !ok {
			// No matching captured project item means there is no safe work-item
			// identity to attach these values to.
			continue
		}
		body, err := projectAsset(page, files)
		if err != nil {
			return err
		}
		if rawString(body, "id") != projectItemID {
			return fmt.Errorf("GitHub project item fields response identity mismatch")
		}
		nodes, err := graphNodes(body, "fieldValues")
		if err != nil {
			return fmt.Errorf("invalid GitHub project field values response")
		}
		for i, nodeRaw := range nodes {
			node, err := DecodeObject(nodeRaw)
			if err != nil {
				return err
			}
			fieldObject, err := DecodeObject(node["field"])
			if err != nil {
				// Unsupported native value shapes remain preserved in discovery.
				continue
			}
			fieldID := rawString(fieldObject, "id")
			if fieldID == "" {
				continue
			}
			values = append(values, projectValueRef{ProjectItemID: projectItemID, ProjectID: association.ProjectID, FieldID: fieldID, Value: append(json.RawMessage(nil), nodeRaw...), AssetSHA256: page.Asset.SHA256, JSONPointer: "/data/node/fieldValues/nodes/" + strconv.Itoa(i)})
		}
	}

	itemByID := map[string]int{}
	for i := range snapshot.Items {
		item := &snapshot.Items[i]
		if item.Identity.Provider != "github" {
			continue
		}
		if _, exists := itemByID[item.Identity.RemoteID]; exists {
			return fmt.Errorf("duplicate normalized GitHub item identity")
		}
		itemByID[item.Identity.RemoteID] = i
	}
	seen := map[string]json.RawMessage{}
	for _, field := range values {
		itemIndex, exists := itemByID[projectItems[field.ProjectItemID].ContentID]
		if !exists {
			continue
		}
		key := Digest(struct {
			ProjectID string
			FieldID   string
		}{field.ProjectID, field.FieldID})
		fieldKey := "projectFields." + key
		wrapper, err := json.Marshal(projectFieldValue{ProjectID: field.ProjectID, FieldID: field.FieldID, Value: field.Value})
		if err != nil {
			return fmt.Errorf("encode native GitHub project field")
		}
		conflictKey := field.ProjectID + "\x00" + projectItems[field.ProjectItemID].ContentID + "\x00" + field.FieldID
		if previous, exists := seen[conflictKey]; exists && semantic(previous) != semantic(wrapper) {
			return fmt.Errorf("conflicting GitHub project field values for one work item")
		}
		seen[conflictKey] = wrapper
		item := &snapshot.Items[itemIndex]
		if item.ProjectFields == nil {
			item.ProjectFields = map[string]json.RawMessage{}
		}
		if previous, exists := item.ProjectFields[key]; exists && semantic(previous) != semantic(wrapper) {
			return fmt.Errorf("conflicting GitHub project field values for one structural field")
		}
		item.ProjectFields[key] = append(json.RawMessage(nil), wrapper...)
		if item.Provenance == nil {
			item.Provenance = map[string][]Pointer{}
		}
		pointers := item.Provenance[fieldKey]
		pointer := Pointer{AssetSHA256: field.AssetSHA256, JSONPointer: field.JSONPointer}
		foundPointer := false
		for _, existing := range pointers {
			if existing == pointer {
				foundPointer = true
				break
			}
		}
		if !foundPointer {
			item.Provenance[fieldKey] = append(pointers, pointer)
		}
	}
	return nil
}

func mappedNativeField(item Item, fieldID string) (json.RawMessage, bool) {
	const prefix = "projectFields."
	if strings.HasPrefix(fieldID, prefix) {
		key := strings.TrimPrefix(fieldID, prefix)
		if key == "" || item.ProjectFields == nil {
			return nil, false
		}
		value, ok := item.ProjectFields[key]
		return value, ok
	}
	value, ok := item.Fields[fieldID]
	return value, ok
}

func graphVariableID(body []byte) (string, error) {
	root, err := DecodeObject(body)
	if err != nil {
		return "", err
	}
	variables, err := DecodeObject(root["variables"])
	if err != nil {
		return "", err
	}
	id := rawString(variables, "id")
	if id == "" {
		return "", fmt.Errorf("query variable id missing")
	}
	return id, nil
}

func projectAsset(page Page, files map[string][]byte) (map[string]json.RawMessage, error) {
	body, ok := files[page.Asset.Path]
	if !ok || Hash(body) != page.Asset.SHA256 {
		return nil, fmt.Errorf("GitHub project discovery asset integrity failure")
	}
	root, err := DecodeObject(body)
	if err != nil {
		return nil, err
	}
	data, err := DecodeObject(root["data"])
	if err != nil {
		return nil, err
	}
	node, err := DecodeObject(data["node"])
	if err != nil {
		return nil, err
	}
	return node, nil
}

func graphNodes(node map[string]json.RawMessage, connection string) ([]json.RawMessage, error) {
	conn, err := DecodeObject(node[connection])
	if err != nil {
		return nil, err
	}
	var nodes []json.RawMessage
	rawNodes := conn["nodes"]
	if len(rawNodes) == 0 || string(rawNodes) == "null" {
		return nil, fmt.Errorf("GraphQL nodes collection missing")
	}
	if err := json.Unmarshal(rawNodes, &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}
