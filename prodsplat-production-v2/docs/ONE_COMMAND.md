# One-command operation

ProdSplat is packaged so normal users do not install Go, Node, Python, Nerfstudio, COLMAP, or npm on the host.

Ubuntu:

```bash
./product-scan.sh
```

Windows PowerShell with Docker Desktop + WSL2 NVIDIA support:

```powershell
.\product-scan.ps1
```

The launcher creates `.env` with a random internal token on first run, creates the persistent workspace, builds/reuses the Docker images, starts the Go control plane and GPU worker, waits for readiness, and opens the dashboard.

Known third-party compatibility recovery such as the COLMAP GPU matcher retry is inside the worker image and requires no manual install or patch step.
