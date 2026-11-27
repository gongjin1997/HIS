package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCMDBAssetCenterFullLifecycleAPI(t *testing.T) {
	ctx := context.Background()
	router, authSvc, cleanup := setupTestServer(t)
	defer cleanup()

	tok, _, err := authSvc.LoginLocal(ctx, "admin1", "Pass#2026!", "127.0.0.1", "cmdb-test")
	if err != nil {
		t.Fatalf("login admin1: %v", err)
	}

	ts := httptest.NewServer(router)
	defer ts.Close()

	doReq := func(method, path string, body any) *http.Response {
		t.Helper()
		var rdr io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rdr = bytes.NewReader(b)
		}
		req, rerr := http.NewRequestWithContext(ctx, method, ts.URL+path, rdr)
		if rerr != nil {
			t.Fatal(rerr)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, derr := http.DefaultClient.Do(req)
		if derr != nil {
			t.Fatal(derr)
		}
		return resp
	}

	// Create dedicated project
	resp := doReq("POST", "/api/v1/projects", map[string]string{"name": "his-cmdb-prj", "description": "HIS CMDB"})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("create project failed: %d %s", resp.StatusCode, string(raw))
	}
	var prj struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&prj)
	_ = resp.Body.Close()

	// 1. Download XLSX & CSV templates
	resp = doReq("GET", "/api/v1/projects/"+prj.ID+"/hosts:template?format=xlsx", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Disposition"), ".xlsx") {
		t.Fatalf("expected 200 xlsx template, got %d %v", resp.StatusCode, resp.Header)
	}
	_ = resp.Body.Close()

	// 2. Bulk Import Chinese CSV with inline SSH credential creation (no pre-existing Vault credential!)
	cnCSV := "主机名,内网IP,SSH端口,SSH用户,业务系统,集群环境,业务模块,区域机房,安全域,故障域,CPU架构,页大小\n" +
		"his-db-01,10.80.1.11,22,root,HIS核心门诊,prod,关系数据库,北京主中心,核心内网,az-1,arm64,65536\n" +
		"his-app-01,10.80.1.12,22,root,HIS核心门诊,prod,应用网关,北京主中心,DMZ区,az-2,amd64,4096\n"
	resp = doReq("POST", "/api/v1/projects/"+prj.ID+"/hosts:import", map[string]any{
		"format": "csv",
		"data":   cnCSV,
		"dryRun": false,
		"inlineCredential": map[string]any{
			"name":      "inline-his-ssh",
			"kind":      "ssh_password",
			"plaintext": "secret-pass",
		},
	})
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("import failed: %d %s", resp.StatusCode, string(raw))
	}
	var importOut struct {
		Imported int `json:"imported"`
		Updated  int `json:"updated"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&importOut)
	_ = resp.Body.Close()
	if importOut.Imported != 2 {
		t.Fatalf("expected 2 imported hosts, got %+v", importOut)
	}

	// 3. Upsert import (updateExisting=true) updates existing host without conflict
	resp = doReq("POST", "/api/v1/projects/"+prj.ID+"/hosts:import", map[string]any{
		"format":         "csv",
		"data":           "主机名,内网IP,故障域\nhis-db-01,10.80.1.11,az-9\n",
		"updateExisting": true,
		"dryRun":         false,
	})
	var upsertOut struct {
		Imported int `json:"imported"`
		Updated  int `json:"updated"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&upsertOut)
	_ = resp.Body.Close()
	if upsertOut.Updated != 1 {
		t.Fatalf("expected 1 updated host on upsert, got %+v", upsertOut)
	}

	// 4. Create Dynamic Group + Preview + List
	resp = doReq("POST", "/api/v1/projects/"+prj.ID+"/cmdb/groups", map[string]any{
		"name":        "鲲鹏64K数据库组",
		"description": "ARM64 64K 节点",
		"rules": []map[string]string{
			{"field": "arch", "op": "eq", "value": "arm64"},
			{"field": "page_size", "op": "gte", "value": "65536"},
		},
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("create dynamic group failed: %d %s", resp.StatusCode, string(raw))
	}
	_ = resp.Body.Close()

	resp = doReq("GET", "/api/v1/projects/"+prj.ID+"/cmdb/groups", nil)
	var grpList struct {
		Items []struct {
			Name         string `json:"name"`
			MatchedCount int    `json:"matchedCount"`
		} `json:"items"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&grpList)
	_ = resp.Body.Close()
	if len(grpList.Items) != 1 || grpList.Items[0].MatchedCount != 1 {
		t.Fatalf("expected 1 dynamic group matching 1 host, got %+v", grpList.Items)
	}

	// 5. External CMDB Sync Source (LiveOps JSON snapshot) + Run
	resp = doReq("POST", "/api/v1/projects/"+prj.ID+"/cmdb/sync-sources", map[string]any{
		"name":        "liveops-snapshot",
		"kind":        "liveops_json",
		"payloadText": `{"data":[{"host_name":"LiveOps_Node_01","host_innerip":"10.80.2.21","group_name":"急诊模块","region":"灾备中心"}]}`,
		"mapping": map[string]any{
			"defaultBiz": "HIS急诊系统",
			"defaultSet": "dr-cluster",
		},
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("create sync source failed: %d %s", resp.StatusCode, string(raw))
	}
	var syncSrc struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&syncSrc)
	_ = resp.Body.Close()

	resp = doReq("POST", "/api/v1/projects/"+prj.ID+"/cmdb/sync-sources/"+syncSrc.ID+":run", map[string]any{"dryRun": false})
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("run sync source failed: %d %s", resp.StatusCode, string(raw))
	}
	var syncRun struct {
		Imported int `json:"imported"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&syncRun)
	_ = resp.Body.Close()
	if syncRun.Imported != 1 {
		t.Fatalf("expected 1 host synced from LiveOps JSON, got %+v", syncRun)
	}

	// 6. List hosts -> test PUT update + POST batch topology update + GET impact
	resp = doReq("GET", "/api/v1/projects/"+prj.ID+"/hosts", nil)
	var hostList struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&hostList)
	_ = resp.Body.Close()
	if len(hostList.Items) != 3 {
		t.Fatalf("expected 3 hosts total, got %d", len(hostList.Items))
	}

	resp = doReq("PUT", "/api/v1/projects/"+prj.ID+"/hosts/"+hostList.Items[0].ID, map[string]any{
		"owner": "王工",
		"state": "active",
	})
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("update host failed: %d %s", resp.StatusCode, string(raw))
	}
	_ = resp.Body.Close()

	resp = doReq("POST", "/api/v1/projects/"+prj.ID+"/hosts:batch", map[string]any{
		"hostIds": []string{hostList.Items[0].ID, hostList.Items[1].ID},
		"action":  "update_topology",
		"owner":   "运维值班组",
	})
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("batch hosts failed: %d %s", resp.StatusCode, string(raw))
	}
	_ = resp.Body.Close()

	resp = doReq("GET", "/api/v1/projects/"+prj.ID+"/hosts/"+hostList.Items[0].ID+"/impact", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("host impact failed: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 7. CMDB Overview + Export Ledger
	resp = doReq("GET", "/api/v1/projects/"+prj.ID+"/cmdb/overview", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview failed: %d", resp.StatusCode)
	}
	var ov struct {
		TotalHosts int   `json:"totalHosts"`
		BizTree    []any `json:"bizTree"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&ov)
	_ = resp.Body.Close()
	if ov.TotalHosts != 3 || len(ov.BizTree) != 2 {
		t.Fatalf("expected 3 hosts across 2 biz trees, got %+v", ov)
	}

	resp = doReq("GET", "/api/v1/projects/"+prj.ID+"/hosts:export?format=xlsx", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Disposition"), ".xlsx") {
		t.Fatalf("export xlsx failed: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 8. Single host creation with inline SSH credential
	resp = doReq("POST", "/api/v1/projects/"+prj.ID+"/hosts", map[string]any{
		"name":    "inline-single-01",
		"address": "10.80.3.31",
		"sshPort": 22,
		"sshUser": "root",
		"inlineCredential": map[string]any{
			"name":      "inline-single-cred",
			"kind":      "ssh_password",
			"plaintext": "SinglePass#2026",
		},
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("single host inline cred failed: %d %s", resp.StatusCode, string(raw))
	}
	_ = resp.Body.Close()

	// 9. Stack Create -> Update -> Apply (prod awaiting_approval) -> Enriched Ops/Approvals -> Cancel -> Delete Stack
	stackYAML := "apiVersion: kinstall.io/v1alpha1\nkind: Stack\nmetadata:\n  name: demo-his-stack\nspec:\n  instances:\n    - id: cache\n      product: valkey\n      productVersion: \"9.1.4\"\n      deployTarget: host-binary\n"
	resp = doReq("POST", "/api/v1/projects/"+prj.ID+"/stacks", map[string]any{
		"env":      "prod",
		"name":     "demo-his-stack",
		"specYaml": stackYAML,
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("create stack failed: %d %s", resp.StatusCode, string(raw))
	}
	var stCreated struct {
		Stack struct {
			ID string `json:"id"`
		} `json:"stack"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&stCreated)
	_ = resp.Body.Close()

	resp = doReq("PUT", "/api/v1/projects/"+prj.ID+"/stacks/"+stCreated.Stack.ID, map[string]any{
		"env":      "prod",
		"name":     "demo-his-stack",
		"specYaml": stackYAML,
	})
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("update stack failed: %d %s", resp.StatusCode, string(raw))
	}
	_ = resp.Body.Close()

	resp = doReq("POST", "/api/v1/projects/"+prj.ID+"/stacks/"+stCreated.Stack.ID+":apply", nil)
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("apply stack failed: %d %s", resp.StatusCode, string(raw))
	}
	var stackOp struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&stackOp)
	_ = resp.Body.Close()
	if stackOp.State != "awaiting_approval" {
		t.Fatalf("expected prod stack apply awaiting_approval, got %+v", stackOp)
	}

	// Verify enriched operations & approvals include human-readable targetName & requestedByName
	resp = doReq("GET", "/api/v1/projects/"+prj.ID+"/operations", nil)
	var opsList struct {
		Items []struct {
			ID              string `json:"id"`
			TargetName      string `json:"targetName"`
			RequestedByName string `json:"requestedByName"`
		} `json:"items"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&opsList)
	_ = resp.Body.Close()
	if len(opsList.Items) == 0 || opsList.Items[0].TargetName != "demo-his-stack" || !strings.Contains(opsList.Items[0].RequestedByName, "admin1") {
		t.Fatalf("expected enriched op names, got %+v", opsList.Items)
	}

	resp = doReq("GET", "/api/v1/projects/"+prj.ID+"/approvals", nil)
	var apsList struct {
		Items []struct {
			OperationID     string `json:"operationId"`
			TargetName      string `json:"targetName"`
			RequestedByName string `json:"requestedByName"`
		} `json:"items"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&apsList)
	_ = resp.Body.Close()
	if len(apsList.Items) != 1 || apsList.Items[0].TargetName != "demo-his-stack" {
		t.Fatalf("expected enriched approval targetName, got %+v", apsList.Items)
	}

	// Cancel awaiting_approval operation -> clears pending approvals
	resp = doReq("POST", "/api/v1/projects/"+prj.ID+"/operations/"+stackOp.ID+":cancel", map[string]string{"reason": "user withdrew"})
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("cancel operation failed: %d %s", resp.StatusCode, string(raw))
	}
	_ = resp.Body.Close()

	resp = doReq("DELETE", "/api/v1/projects/"+prj.ID+"/stacks/"+stCreated.Stack.ID, nil)
	if resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete stack failed: %d %s", resp.StatusCode, string(raw))
	}
	_ = resp.Body.Close()

	// 10. Instance Create -> GetDetail -> Update
	resp = doReq("POST", "/api/v1/projects/"+prj.ID+"/instances", map[string]any{
		"env":            "dev",
		"name":           "valkey-his-01",
		"product":        "valkey",
		"productVersion": "9.1.4",
		"deployTarget":   "host-binary",
		"desiredSpec":    map[string]any{"topology": "standalone", "targetHosts": []string{hostList.Items[0].ID}},
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("create instance failed: %d %s", resp.StatusCode, string(raw))
	}
	var instCreated struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&instCreated)
	_ = resp.Body.Close()

	resp = doReq("GET", "/api/v1/projects/"+prj.ID+"/instances/"+instCreated.ID, nil)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("get instance detail failed: %d %s", resp.StatusCode, string(raw))
	}
	var instDetail struct {
		AssignedHosts []struct {
			HostID string `json:"hostId"`
		} `json:"assignedHosts"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&instDetail)
	_ = resp.Body.Close()
	if len(instDetail.AssignedHosts) != 1 || instDetail.AssignedHosts[0].HostID != hostList.Items[0].ID {
		t.Fatalf("expected 1 assigned host in instance detail, got %+v", instDetail)
	}

	resp = doReq("PUT", "/api/v1/projects/"+prj.ID+"/instances/"+instCreated.ID, map[string]any{
		"desiredSpec": map[string]any{"topology": "standalone", "port": 6380, "targetHosts": []string{hostList.Items[0].ID}},
	})
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("update instance failed: %d %s", resp.StatusCode, string(raw))
	}
	_ = resp.Body.Close()

	// 11. Notification channel Create -> Delete
	resp = doReq("POST", "/api/v1/projects/"+prj.ID+"/notification-channels", map[string]any{
		"name": "ops-hook", "kind": "webhook", "enabled": true, "events": []string{"*"}, "config": map[string]string{"url": "http://127.0.0.1:9"},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create notification channel failed: %d", resp.StatusCode)
	}
	var nc struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&nc)
	_ = resp.Body.Close()
	resp = doReq("DELETE", "/api/v1/projects/"+prj.ID+"/notification-channels/"+nc.ID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete notification channel failed: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
