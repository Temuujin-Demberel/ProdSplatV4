# Windows / Docker Desktop

ProdSplat's containers are Linux containers. On Windows, Docker Desktop runs them through WSL2.

## Required setup

1. Install a current NVIDIA Windows driver that supports CUDA in WSL2.
2. Enable/install WSL2.
3. Install Docker Desktop.
4. Ensure Docker Desktop is using the WSL2 backend.
5. Start Docker Desktop before running ProdSplat.

## Start

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\product-scan.ps1
```

PowerShell does not require `chmod`.

## GPU diagnostic

```powershell
.\scripts\doctor.ps1
```

The diagnostic builds/runs the worker image and checks `torch.cuda.is_available()` plus the worker's pinned software versions.

## Performance note

Keep the project on a normal Windows drive such as `D:\Projects\ProdSplat`. The bind-mounted workspace must have enough disk space for extracted frames, Nerfstudio training outputs, multiple attempts, and renders. A product job can occupy many gigabytes depending on capture resolution and retained training data.
