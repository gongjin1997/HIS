package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type TreeKind string

const (
	TreeBusiness TreeKind = "business"
	TreeResource TreeKind = "resource"
	TreeCustom   TreeKind = "custom"
)

func (k TreeKind) Valid() bool {
	return k == TreeBusiness || k == TreeResource || k == TreeCustom
}

func (k TreeKind) IsSingleParent() bool {
	return k == TreeBusiness || k == TreeResource
}

type ModelGroup struct {
	TenantID  string    `json:"tenant_id"`
	AppID     string    `json:"app_id"`
	GroupID   string    `json:"group_id"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}

type ModelField struct {
	TenantID     string   `json:"tenant_id"`
	AppID        string   `json:"app_id"`
	ModelID      string   `json:"model_id"`
	FieldKey     string   `json:"field_key"`
	Name         string   `json:"name"`
	FieldType    string   `json:"field_type"` // string | number | boolean | enum | secret | json
	Required     bool     `json:"required"`
	IsUnique     bool     `json:"is_unique"`
	EnumValues   []string `json:"enum_values"`
	DefaultValue string   `json:"default_value"`
	SortOrder    int      `json:"sort_order"`
}

type AssetModel struct {
	TenantID      string       `json:"tenant_id"`
	AppID         string       `json:"app_id"`
	ModelID       string       `json:"model_id"`
	GroupID       string       `json:"group_id"`
	Code          string       `json:"code"`
	Name          string       `json:"name"`
	Icon          string       `json:"icon"`
	IsBuiltin     bool         `json:"is_builtin"`
	Description   string       `json:"description"`
	Fields        []ModelField `json:"fields,omitempty"`
	InstanceCount int          `json:"instance_count"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

type RelationType struct {
	TenantID      string    `json:"tenant_id"`
	AppID         string    `json:"app_id"`
	RelTypeID     string    `json:"rel_type_id"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	SourceModelID string    `json:"source_model_id"`
	TargetModelID string    `json:"target_model_id"`
	Cardinality   string    `json:"cardinality"` // 1:N | N:N
	CreatedAt     time.Time `json:"created_at"`
}

type AssetInstance struct {
	TenantID   string         `json:"tenant_id"`
	AppID      string         `json:"app_id"`
	InstanceID string         `json:"instance_id"`
	ModelID    string         `json:"model_id"`
	ModelCode  string         `json:"model_code,omitempty"`
	ModelName  string         `json:"model_name,omitempty"`
	Name       string         `json:"name"`
	UniqueKey  string         `json:"unique_key"`
	Status     string         `json:"status"` // ACTIVE | MAINTENANCE | RETIRED
	Attrs      map[string]any `json:"attrs"`
	Version    int64          `json:"version"`
	CreatedBy  string         `json:"created_by"`
	UpdatedBy  string         `json:"updated_by"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type AssetEdge struct {
	TenantID  string         `json:"tenant_id"`
	AppID     string         `json:"app_id"`
	EdgeID    string         `json:"edge_id"`
	TreeKind  TreeKind       `json:"tree_kind"`
	RelTypeID string         `json:"rel_type_id"`
	SourceID  string         `json:"source_id"`
	TargetID  string         `json:"target_id"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type DeleteOperation struct {
	TenantID       string    `json:"tenant_id"`
	AppID          string    `json:"app_id"`
	OpID           string    `json:"op_id"`
	RootInstanceID string    `json:"root_instance_id"`
	State          string    `json:"state"` // RUNNING | COMPLETED | FAILED
	TotalItems     int       `json:"total_items"`
	DeletedItems   int       `json:"deleted_items"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TopologyGraph holds nodes and edges for dual business/resource topology rendering.
type TopologyGraph struct {
	TreeKind  TreeKind        `json:"tree_kind"`
	Instances []AssetInstance `json:"instances"`
	Edges     []AssetEdge     `json:"edges"`
}

// ValidateInstanceAttrs validates dynamic CMDB instance fields against model schema and returns unique field pairs.
func ValidateInstanceAttrs(fields []ModelField, attrs map[string]any) (map[string]any, map[string]string, error) {
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		out[k] = v
	}
	uniques := make(map[string]string)
	for _, f := range fields {
		val, exists := out[f.FieldKey]
		if (!exists || val == nil || strings.TrimSpace(fmt.Sprint(val)) == "") && f.DefaultValue != "" {
			val = f.DefaultValue
			out[f.FieldKey] = val
			exists = true
		}
		strVal := ""
		if exists && val != nil {
			strVal = strings.TrimSpace(fmt.Sprint(val))
		}
		if f.Required && strVal == "" {
			return nil, nil, ValidationError(fmt.Sprintf("field %q (%s) is required", f.FieldKey, f.Name))
		}
		if strVal != "" && f.FieldType == "enum" && len(f.EnumValues) > 0 {
			if !slices.Contains(f.EnumValues, strVal) {
				return nil, nil, ValidationError(fmt.Sprintf("field %q value %q is not in allowed enum %v", f.FieldKey, strVal, f.EnumValues))
			}
		}
		if f.IsUnique && strVal != "" {
			uniques[f.FieldKey] = strVal
		}
	}
	return out, uniques, nil
}
