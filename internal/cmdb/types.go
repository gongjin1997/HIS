package cmdb

type ModelGroup struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	SortOrder   int    `json:"sortOrder"`
	CreatedAt   string `json:"createdAt"`
}

type Model struct {
	ID            string      `json:"id"`
	WorkspaceID   string      `json:"workspaceId"`
	GroupCode     string      `json:"groupCode"`
	Code          string      `json:"code"`
	Name          string      `json:"name"`
	Icon          string      `json:"icon"`
	Description   string      `json:"description"`
	IsBuiltin     bool        `json:"isBuiltin"`
	PosX          float64     `json:"posX"`
	PosY          float64     `json:"posY"`
	InstanceCount int         `json:"instanceCount"`
	Attrs         []ModelAttr `json:"attrs,omitempty"`
	CreatedAt     string      `json:"createdAt"`
	UpdatedAt     string      `json:"updatedAt"`
}

type ModelAttr struct {
	ID           string   `json:"id"`
	WorkspaceID  string   `json:"workspaceId"`
	ModelCode    string   `json:"modelCode"`
	AttrKey      string   `json:"attrKey"`
	Name         string   `json:"name"`
	FieldType    string   `json:"fieldType"` // string | int | float | bool | enum | ip | date | json
	GroupName    string   `json:"groupName"`
	Required     bool     `json:"required"`
	Readonly     bool     `json:"readonly"`
	IsUnique     bool     `json:"isUnique"`
	DefaultValue string   `json:"defaultValue"`
	Options      []string `json:"options"`
	SortOrder    int      `json:"sortOrder"`
	CreatedAt    string   `json:"createdAt"`
}

type ModelRelation struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspaceId"`
	SourceModel  string `json:"sourceModel"`
	TargetModel  string `json:"targetModel"`
	RelationCode string `json:"relationCode"`
	RelationName string `json:"relationName"`
	Cardinality  string `json:"cardinality"`
	CreatedAt    string `json:"createdAt"`
}

// HostEndpoint is the 1:1 execution & telemetry extension attached to every m_host CI instance.
type HostEndpoint struct {
	CIID         string         `json:"ciId"`
	IP           string         `json:"ip"`
	SSHPort      int            `json:"sshPort"`
	SSHUser      string         `json:"sshUser"`
	CredentialID string         `json:"credentialId"`
	ExecMode     string         `json:"execMode"` // auto | agent | ssh
	AgentID      string         `json:"agentId"`
	AgentStatus  string         `json:"agentStatus"` // ONLINE | OFFLINE | REVOKED
	OSFamily     string         `json:"osFamily"`
	OSVersion    string         `json:"osVersion"`
	Kernel       string         `json:"kernel"`
	Arch         string         `json:"arch"`
	CPUCores     int            `json:"cpuCores"`
	MemMB        int            `json:"memMb"`
	DiskGB       int            `json:"diskGb"`
	LoadAvg      string         `json:"loadAvg"`
	CPUUsagePct  float64        `json:"cpuUsagePct"`
	MemUsagePct  float64        `json:"memUsagePct"`
	Facts        map[string]any `json:"facts"`
	LastSeenAt   string         `json:"lastSeenAt"`
	UpdatedAt    string         `json:"updatedAt"`
}

type CIInstance struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspaceId"`
	ModelCode   string         `json:"modelCode"`
	UniqueKey   string         `json:"uniqueKey"`
	Name        string         `json:"name"`
	Env         string         `json:"env"`
	Status      string         `json:"status"`
	Owner       string         `json:"owner"`
	Tags        []string       `json:"tags"`
	Data        map[string]any `json:"data"`
	Host        *HostEndpoint  `json:"host,omitempty"`
	TopoNodeIDs []string       `json:"topoNodeIds,omitempty"`
	CreatedAt   string         `json:"createdAt"`
	UpdatedAt   string         `json:"updatedAt"`
}

type CIRelation struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspaceId"`
	SourceCIID   string `json:"sourceCiId"`
	TargetCIID   string `json:"targetCiId"`
	RelationCode string `json:"relationCode"`
	Note         string `json:"note"`
	CreatedAt    string `json:"createdAt"`
}

type CIHistory struct {
	ID         string         `json:"id"`
	CIID       string         `json:"ciId"`
	Action     string         `json:"action"`
	Actor      string         `json:"actor"`
	BeforeData map[string]any `json:"beforeData"`
	AfterData  map[string]any `json:"afterData"`
	CreatedAt  string         `json:"createdAt"`
}

type LineageGraph struct {
	RootCIID string       `json:"rootCiId"`
	Nodes    []CIInstance `json:"nodes"`
	Edges    []CIRelation `json:"edges"`
}

type ServiceTemplate struct {
	ID          string            `json:"id"`
	WorkspaceID string            `json:"workspaceId"`
	Code        string            `json:"code"`
	Name        string            `json:"name"`
	Category    string            `json:"category"`
	Ports       []int             `json:"ports"`
	Processes   []string          `json:"processes"`
	EnvDefaults map[string]string `json:"envDefaults"`
	Description string            `json:"description"`
	CreatedAt   string            `json:"createdAt"`
	UpdatedAt   string            `json:"updatedAt"`
}

type TopologyNode struct {
	ID                string            `json:"id"`
	WorkspaceID       string            `json:"workspaceId"`
	TreeType          string            `json:"treeType"` // business | resource
	ParentID          string            `json:"parentId"`
	NodeType          string            `json:"nodeType"` // biz | set | module | org | region | zone
	Name              string            `json:"name"`
	Code              string            `json:"code"`
	ServiceTemplateID string            `json:"serviceTemplateId"`
	Labels            map[string]string `json:"labels"`
	SortOrder         int               `json:"sortOrder"`
	CICount           int               `json:"ciCount"`
	Children          []*TopologyNode   `json:"children"`
}

type DynamicCondition struct {
	Field string `json:"field"`
	Op    string `json:"op"` // eq | neq | contains | in | gte | lte
	Value string `json:"value"`
}

type DynamicGroup struct {
	ID          string             `json:"id"`
	WorkspaceID string             `json:"workspaceId"`
	Name        string             `json:"name"`
	ModelCode   string             `json:"modelCode"`
	Conditions  []DynamicCondition `json:"conditions"`
	Description string             `json:"description"`
	MatchCount  int                `json:"matchCount"`
	CreatedAt   string             `json:"createdAt"`
	UpdatedAt   string             `json:"updatedAt"`
}

type Rack struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspaceId"`
	Datacenter  string     `json:"datacenter"`
	Room        string     `json:"room"`
	Name        string     `json:"name"`
	UHeight     int        `json:"uHeight"`
	PowerKW     float64    `json:"powerKw"`
	Status      string     `json:"status"`
	Notes       string     `json:"notes"`
	UsedU       int        `json:"usedU"`
	UsedPowerW  int        `json:"usedPowerW"`
	Units       []RackUnit `json:"units,omitempty"`
	CreatedAt   string     `json:"createdAt"`
	UpdatedAt   string     `json:"updatedAt"`
}

type RackUnit struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	RackID      string `json:"rackId"`
	CIID        string `json:"ciId"`
	DeviceName  string `json:"deviceName"`
	ModelCode   string `json:"modelCode"`
	UStart      int    `json:"uStart"`
	USize       int    `json:"uSize"`
	Face        string `json:"face"` // front | rear
	PowerW      int    `json:"powerW"`
	ColorTag    string `json:"colorTag"`
	CreatedAt   string `json:"createdAt"`
}

type IPAMPrefix struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspaceId"`
	CIDR        string  `json:"cidr"`
	VLANID      int     `json:"vlanId"`
	Site        string  `json:"site"`
	VRF         string  `json:"vrf"`
	Gateway     string  `json:"gateway"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	TotalIPs    int     `json:"totalIps"`
	UsedIPs     int     `json:"usedIps"`
	UtilPct     float64 `json:"utilPct"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
}

type IPAMAddress struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	PrefixID    string `json:"prefixId"`
	IP          string `json:"ip"`
	Status      string `json:"status"`
	CIID        string `json:"ciId"`
	DNSName     string `json:"dnsName"`
	MACAddr     string `json:"macAddr"`
	Owner       string `json:"owner"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type ImportResult struct {
	ModelCode string   `json:"modelCode"`
	Created   int      `json:"created"`
	Updated   int      `json:"updated"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors"`
}

type ModelPosition struct {
	Code string  `json:"code"`
	PosX float64 `json:"posX"`
	PosY float64 `json:"posY"`
}

type UpsertCIRequest struct {
	ModelCode string         `json:"modelCode"`
	UniqueKey string         `json:"uniqueKey"`
	Name      string         `json:"name"`
	Env       string         `json:"env"`
	Status    string         `json:"status"`
	Owner     string         `json:"owner"`
	Tags      []string       `json:"tags"`
	Data      map[string]any `json:"data"`
	Host      *HostEndpoint  `json:"host,omitempty"`
}

type OverviewStats struct {
	TotalModels     int            `json:"totalModels"`
	TotalCIs        int            `json:"totalCis"`
	TotalHosts      int            `json:"totalHosts"`
	OnlineAgents    int            `json:"onlineAgents"`
	TotalRacks      int            `json:"totalRacks"`
	TotalPrefixes   int            `json:"totalPrefixes"`
	AllocatedIPs    int            `json:"allocatedIps"`
	TotalConfigs    int            `json:"totalConfigs"`
	TotalPlugins    int            `json:"totalPlugins"`
	TotalReleases   int            `json:"totalReleases"`
	TotalArtifacts  int            `json:"totalArtifacts"`
	ModelBreakdown  map[string]int `json:"modelBreakdown"`
	EnvBreakdown    map[string]int `json:"envBreakdown"`
	RecentAuditLogs int            `json:"recentAuditLogs"`
}
