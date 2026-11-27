package cmdb

import "time"

// FieldType enumerates supported dynamic meta-model attribute types (bk-cmdb / LiveOps 03.3).
type FieldType string

const (
	FieldString FieldType = "string"
	FieldInt    FieldType = "int"
	FieldLong   FieldType = "long"
	FieldDouble FieldType = "double"
	FieldBool   FieldType = "bool"
	FieldDate   FieldType = "date"
	FieldEnum   FieldType = "enum"
	FieldIP     FieldType = "ip"
	FieldSecret FieldType = "secret"
)

type ModelGroup struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sortOrder"`
	IsBuiltin bool      `json:"isBuiltin"`
	CreatedAt time.Time `json:"createdAt"`
}

type Model struct {
	ID                 string           `json:"id"`
	ProjectID          string           `json:"projectId"`
	GroupCode          string           `json:"groupCode"`
	Code               string           `json:"code"`
	Name               string           `json:"name"`
	Icon               string           `json:"icon"`
	Description        string           `json:"description"`
	PosX               float64          `json:"posX"`
	PosY               float64          `json:"posY"`
	IsBuiltin          bool             `json:"isBuiltin"`
	IsMainline         bool             `json:"isMainline"`
	MainlineParentCode string           `json:"mainlineParentCode,omitempty"`
	InstanceCount      int              `json:"instanceCount"`
	Attributes         []ModelAttribute `json:"attributes,omitempty"`
	CreatedAt          time.Time        `json:"createdAt"`
	UpdatedAt          time.Time        `json:"updatedAt"`
}

type ModelAttribute struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"projectId"`
	ModelCode         string    `json:"modelCode"`
	AttrGroup         string    `json:"attrGroup"`
	PropID            string    `json:"propId"`
	PropName          string    `json:"propName"`
	FieldType         FieldType `json:"fieldType"`
	Unit              string    `json:"unit,omitempty"`
	Required          bool      `json:"required"`
	Readonly          bool      `json:"readonly"`
	IsUnique          bool      `json:"isUnique"`
	Display           bool      `json:"display"`
	EnumOptions       []string  `json:"enumOptions,omitempty"`
	DefaultValue      string    `json:"defaultValue,omitempty"`
	SourceKind        string    `json:"sourceKind"` // manual | collect | scan
	CollectorPluginID string    `json:"collectorPluginId,omitempty"`
	SortOrder         int       `json:"sortOrder"`
}

type ModelRelation struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"projectId"`
	SourceModel  string    `json:"sourceModel"`
	TargetModel  string    `json:"targetModel"`
	RelationType string    `json:"relationType"` // runs_on | belongs_to | depends_on | connects_to | mounted_in | routes_to
	Label        string    `json:"label"`
	MappingKind  string    `json:"mappingKind"` // 1:1 | 1:N | N:N
	CreatedAt    time.Time `json:"createdAt"`
}

type MetaModelOverview struct {
	Groups    []ModelGroup    `json:"groups"`
	Models    []Model         `json:"models"`
	Relations []ModelRelation `json:"relations"`
}

type CIInstance struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"projectId"`
	ModelCode string         `json:"modelCode"`
	ModelName string         `json:"modelName,omitempty"`
	Name      string         `json:"name"`
	UniqueKey string         `json:"uniqueKey,omitempty"`
	Status    string         `json:"status"`
	Env       string         `json:"env"`
	Owner     string         `json:"owner"`
	Attrs     map[string]any `json:"attrs"`
	Version   int            `json:"version"`
	CreatedBy string         `json:"createdBy,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type CIRelation struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"projectId"`
	SourceCIID   string    `json:"sourceCiId"`
	TargetCIID   string    `json:"targetCiId"`
	RelationType string    `json:"relationType"`
	Label        string    `json:"label"`
	CreatedAt    time.Time `json:"createdAt"`
}

type CIHistoryEntry struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"projectId"`
	CIID      string         `json:"ciId"`
	ModelCode string         `json:"modelCode"`
	Action    string         `json:"action"` // create | update | delete | collect
	Before    map[string]any `json:"before"`
	After     map[string]any `json:"after"`
	Actor     string         `json:"actor"`
	CreatedAt time.Time      `json:"createdAt"`
}

type CIGraphNode struct {
	ID        string `json:"id"`
	ModelCode string `json:"modelCode"`
	ModelName string `json:"modelName"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Env       string `json:"env"`
	Depth     int    `json:"depth"`
	Direction string `json:"direction"` // root | upstream | downstream
}

type CIGraph struct {
	RootID string        `json:"rootId"`
	Nodes  []CIGraphNode `json:"nodes"`
	Edges  []CIRelation  `json:"edges"`
}

type ServiceTemplateProcess struct {
	Name        string `json:"name"`
	Binary      string `json:"binary"`
	User        string `json:"user"`
	BindPort    int    `json:"bindPort"`
	Protocol    string `json:"protocol"`
	StartCmd    string `json:"startCmd,omitempty"`
	HealthCheck string `json:"healthCheck,omitempty"`
}

type ServiceTemplate struct {
	ID            string                   `json:"id"`
	ProjectID     string                   `json:"projectId"`
	Name          string                   `json:"name"`
	Category      string                   `json:"category"`
	Description   string                   `json:"description"`
	Processes     []ServiceTemplateProcess `json:"processes"`
	Ports         []int                    `json:"ports"`
	DefaultParams map[string]any           `json:"defaultParams"`
	CreatedAt     time.Time                `json:"createdAt"`
	UpdatedAt     time.Time                `json:"updatedAt"`
}

type TopologyNode struct {
	ID                string         `json:"id"`
	ProjectID         string         `json:"projectId"`
	TopoKind          string         `json:"topoKind"` // business | resource
	ParentID          string         `json:"parentId"`
	ModelCode         string         `json:"modelCode"`
	Name              string         `json:"name"`
	ServiceTemplateID string         `json:"serviceTemplateId,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	SortOrder         int            `json:"sortOrder"`
	BoundHostCount    int            `json:"boundHostCount"`
	Children          []TopologyNode `json:"children,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
}

// DCIM & IPAM (NetBox-inspired)
type Rack struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"projectId"`
	SiteName    string     `json:"siteName"`
	RoomName    string     `json:"roomName"`
	Name        string     `json:"name"`
	UHeight     int        `json:"uHeight"`
	PowerKW     float64    `json:"powerKw"`
	Status      string     `json:"status"`
	Notes       string     `json:"notes"`
	UsedU       int        `json:"usedU"`
	UsedPowerW  int        `json:"usedPowerW"`
	UtilPercent float64    `json:"utilPercent"`
	Units       []RackUnit `json:"units,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type RackUnit struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"projectId"`
	RackID     string    `json:"rackId"`
	CIID       string    `json:"ciId,omitempty"`
	DeviceName string    `json:"deviceName"`
	DeviceRole string    `json:"deviceRole"` // server | switch | firewall | storage | pdu
	StartU     int       `json:"startU"`
	USize      int       `json:"uSize"`
	PowerW     int       `json:"powerW"`
	Status     string    `json:"status"`
	ColorTag   string    `json:"colorTag"` // primary | ok | warn | accent | info
	CreatedAt  time.Time `json:"createdAt"`
}

type IPAMPrefix struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	CIDR        string    `json:"cidr"`
	VLANID      int       `json:"vlanId"`
	VRF         string    `json:"vrf"`
	SiteName    string    `json:"siteName"`
	Gateway     string    `json:"gateway"`
	Role        string    `json:"role"`
	Description string    `json:"description"`
	TotalUsable int       `json:"totalUsable"`
	UsedCount   int       `json:"usedCount"`
	UtilPercent float64   `json:"utilPercent"`
	CreatedAt   time.Time `json:"createdAt"`
}

type IPAMAddress struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	PrefixID    string    `json:"prefixId"`
	IPAddress   string    `json:"ipAddress"`
	Status      string    `json:"status"` // active | reserved | dhcp | deprecated
	CIID        string    `json:"ciId,omitempty"`
	HostID      string    `json:"hostId,omitempty"`
	Hostname    string    `json:"hostname"`
	MACAddress  string    `json:"macAddress"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type ExcelImportRowResult struct {
	Row     int    `json:"row"`
	Name    string `json:"name"`
	Action  string `json:"action"` // create | update | error
	Message string `json:"message,omitempty"`
}

type ExcelImportReport struct {
	ModelCode string                 `json:"modelCode"`
	DryRun    bool                   `json:"dryRun"`
	TotalRows int                    `json:"totalRows"`
	Created   int                    `json:"created"`
	Updated   int                    `json:"updated"`
	Errors    int                    `json:"errors"`
	Rows      []ExcelImportRowResult `json:"rows"`
}

type CollectorPluginSpec struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	TargetModel string   `json:"targetModel"`
	Fields      []string `json:"fields"`
	ReadOnly    bool     `json:"readOnly"`
}

type OverviewStats struct {
	TotalModels     int              `json:"totalModels"`
	TotalCIs        int              `json:"totalCis"`
	TotalRelations  int              `json:"totalRelations"`
	TotalRacks      int              `json:"totalRacks"`
	TotalRackU      int              `json:"totalRackU"`
	UsedRackU       int              `json:"usedRackU"`
	TotalPrefixes   int              `json:"totalPrefixes"`
	AllocatedIPs    int              `json:"allocatedIps"`
	TotalUsableIPs  int              `json:"totalUsableIps"`
	ModelBreakdown  map[string]int   `json:"modelBreakdown"`
	RecentHistories []CIHistoryEntry `json:"recentHistories"`
}
