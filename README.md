# kinstall

> **Go 1.27 (`CGO_ENABLED=0`) single-binary, agentless (SSH), offline-first infrastructure installation, lifecycle & CMDB platform.**

## Documentation (`v4.5-complete`)

The complete design documentation archive is located under [`docs/`](./docs/README.md) with the single-source-of-truth OS matrix under [`spec/`](./spec/):

- **Architecture Panorama & SSOT**: [`docs/ARCHITECTURE.md`](./docs/ARCHITECTURE.md)
- **Full-Stack Engineering Task Checklist (126 Tasks, M0–M5)**: [`docs/DEV_TASK_CHECKLIST.md`](./docs/DEV_TASK_CHECKLIST.md)
- **Development Handbook & Go Interfaces**: [`docs/05-开发文档.md`](./docs/05-开发文档.md)
- **UI Design System (v2.0)**: [`docs/DESIGN.md`](./docs/DESIGN.md)
- **Web Console IA & Frontend Engineering**: [`docs/WEB_DESIGN.md`](./docs/WEB_DESIGN.md)
- **Deep Design Series**: [`docs/deep-design/`](./docs/deep-design/) (`D1–D3`, `G1–G3`, `P1–P10`, `R1–R2`)
- **OS Matrix & Package Map SSOT**: [`spec/osmatch.yaml`](./spec/osmatch.yaml), [`spec/pkgmap.yaml`](./spec/pkgmap.yaml), [`spec/osmatch_golden.yaml`](./spec/osmatch_golden.yaml)

## Quick Verification

```bash
python3 spec/gen_os_docs.py --check
make lint test build
```
