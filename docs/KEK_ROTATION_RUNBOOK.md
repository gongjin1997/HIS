# KEK 信封主密钥零停机轮换手册（`KEK_ROTATION_RUNBOOK.md` · O-11 / T-M1-02）

> 对应架构约束：`D1 §3.2`、`ARCHITECTURE.md §12.4 O-11`、`DEV_TASK_CHECKLIST.md T-M1-02`。

## 1. 密钥分层架构与安全红线

1. **DEK（数据加密密钥）**：每条凭据独立随机生成 256-bit DEK，采用 `AES-256-GCM`（默认）或国密 `SM4-GCM`（`KINSTALL_GM_ENABLED=true`，`emmansun/gmsm`）加密明文，并强绑定 AAD（`table:record_id:key_version`）。
2. **KEK（主封装密钥）**：支持三后端：
   - **本地密钥环文件**（默认 `$KINSTALL_DATA_DIR/master.key`，权限强制 `0600`，支持多版本 JSON Keyring）；
   - **环境变量注入**（`KINSTALL_KEK_BASE64`）；
   - **OpenBao 2.7.2 Transit 引擎**（强制 `derived=true` + `context` 参数规避 `/rewrap` AAD 缺陷）。
3. **绝对红线**：轮换全过程**零明文落盘、零日志输出、旧版本密钥在所有记录完成重包装前严禁删除**。

## 2. 本地密钥环文件轮换 SOP

1. **备份旧密钥环与数据库**：
   ```bash
   cp -p ~/.kinstall/master.key ~/.kinstall/master.key.bak.$(date +%Y%m%d%H%M%S)
   chmod 0600 ~/.kinstall/master.key.bak.*
   ```
2. **追加新版本 KEK（保留历史版本供解密）**：在密钥环中新增 `version = current + 1` 的 32 字节随机密钥并设为 `activeVersion`。
3. **滚动重启控制面节点**：确认 `/readyz` 返回 `200 OK`。
4. **执行在线重包装（CLI 或 Web 控制台「系统设置 → 凭据保险库」）**：
   ```bash
   kinstall secrets rewrap --project <prj_id>
   # 远程模式：
   kinstall --server https://kinstall.corp.local --token "$KINSTALL_TOKEN" secrets rewrap --project <prj_id>
   ```
5. **审计核验**：运行 `kinstall audit verify`，确认 `secrets.rewrap` 已写入防篡改链且 `valid: true`。

## 3. OpenBao 2.7.2 Transit 轮换 SOP

1. 执行密钥版本轮换：`bao write -f transit/keys/kinstall-kek/rotate`。
2. 调用 `kinstall secrets rewrap --project <prj_id>`，通过 Transit `derived=true` + `context=base64(table:id)` 将全部信封重包装至最新版本。
3. 所有项目完成后按合规周期提升 `min_decryption_version`。
