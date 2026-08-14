<#
Manual Windows E2E for the eraser npipe transport.

Scales the long-lived AKS Windows pool 0 -> 1, builds hack/winspike from the
CURRENT working tree, runs it in a HostProcess pod against containerd's named
pipe, checks the least-privilege assumption, then scales back to 0.

  pwsh run-windows-e2e.ps1              # full run, scales back down
  pwsh run-windows-e2e.ps1 -KeepNode    # leave the node up for iteration
  pwsh run-windows-e2e.ps1 -SkipScaleUp # node already running
#>
[CmdletBinding()]
param(
    [string]$Repo = 'q:\src\eraser',
    [string]$ResourceGroup = 'rg-eraser-2mgr-test',
    [string]$Cluster = 'aks-eraser-win',
    [string]$WindowsPool = 'npwin',
    [string]$GoBin = 'C:\Users\yuewu2\gosdk\go\bin',
    [switch]$SkipScaleUp,
    [switch]$KeepNode
)

$ErrorActionPreference = 'Stop'
$env:PATH = "$GoBin;$env:PATH"
$failed = $false

function Step($msg) { Write-Host "`n=== $msg ===" -ForegroundColor Cyan }

try {
    Step 'code under test'
    git -C $Repo --no-pager log --oneline -1
    git -C $Repo status --short

    if (-not $SkipScaleUp) {
        Step 'scale windows pool to 1'
        az aks nodepool scale -g $ResourceGroup --cluster-name $Cluster -n $WindowsPool --node-count 1 -o none
    }

    Step 'kubeconfig'
    az aks get-credentials -g $ResourceGroup -n $Cluster --overwrite-existing --only-show-errors

    Step 'wait for a ready windows node'
    $node = $null
    foreach ($i in 1..60) {
        $node = kubectl get nodes -l kubernetes.io/os=windows `
            -o jsonpath='{.items[?(@.status.conditions[-1].type=="Ready")].metadata.name}'
        if ($node) { break }
        Write-Host "  waiting ($i/60)"
        Start-Sleep -Seconds 15
    }
    if (-not $node) { throw 'no windows node became ready' }
    Write-Host "  node: $node"

    Step 'build checker for windows/amd64'
    Push-Location $Repo
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'
    go build -ldflags="-s -w" -o "$PSScriptRoot\winspike.exe" ./hack/winspike
    if ($LASTEXITCODE -ne 0) { throw 'build failed' }
    $env:GOOS = ''; $env:GOARCH = ''
    Pop-Location
    '  size = {0:N1} MB' -f ((Get-Item "$PSScriptRoot\winspike.exe").Length / 1MB)

    Step 'start HostProcess pod'
    kubectl delete pod winspike winspike-lowpriv --ignore-not-found --wait=false | Out-Null
    Start-Sleep -Seconds 5
    $pod = @"
apiVersion: v1
kind: Pod
metadata:
  name: winspike
spec:
  restartPolicy: Never
  hostNetwork: true
  nodeSelector:
    kubernetes.io/os: windows
  securityContext:
    windowsOptions:
      hostProcess: true
      runAsUserName: "NT AUTHORITY\\SYSTEM"
  containers:
    - name: shell
      image: mcr.microsoft.com/windows/nanoserver:ltsc2022
      command: ["powershell.exe", "-NoProfile", "-Command", "Start-Sleep -Seconds 2700"]
"@
    $pod | kubectl apply -f -
    kubectl wait --for=condition=Ready pod/winspike --timeout=15m

    Step 'copy checker onto the node'
    Push-Location $PSScriptRoot
    kubectl cp winspike.exe winspike:winspike.exe
    Pop-Location

    Step 'list and delete the largest unused image over npipe'
    kubectl exec winspike -- powershell.exe -NoProfile -Command 'C:\hpc\winspike.exe -delete-largest-unused'
    if ($LASTEXITCODE -ne 0) { $failed = $true; Write-Host 'CHECKER FAILED' -ForegroundColor Red }

    Step 'least-privilege regression (LocalService must be denied)'
    # Uses the node's own crictl rather than the copied binary: each HostProcess
    # pod gets a separate sandbox mount, so C:\hpc is not shared between pods.
    $lowpriv = @"
apiVersion: v1
kind: Pod
metadata:
  name: winspike-lowpriv
spec:
  restartPolicy: Never
  hostNetwork: true
  nodeSelector:
    kubernetes.io/os: windows
  securityContext:
    windowsOptions:
      hostProcess: true
      runAsUserName: "NT AUTHORITY\\LocalService"
  containers:
    - name: probe
      image: mcr.microsoft.com/windows/nanoserver:ltsc2022
      command:
        - powershell.exe
        - -NoProfile
        - -Command
        - "& 'C:\\Program Files\\containerd\\crictl.exe' --image-endpoint 'npipe://./pipe/containerd-containerd' images"
"@
    $lowpriv | kubectl apply -f -
    kubectl wait --for=jsonpath='{.status.phase}'=Failed pod/winspike-lowpriv --timeout=5m 2>&1 | Out-Null
    $log = kubectl logs winspike-lowpriv 2>&1 | Out-String
    if ($log -match 'Access is denied') {
        Write-Host '  expected: LocalService denied on the containerd pipe' -ForegroundColor Green
    }
    else {
        Write-Host '  LocalService was NOT denied - re-check the least-privilege assumption' -ForegroundColor Red
        Write-Host $log
        $failed = $true
    }
}
finally {
    Step 'cleanup'
    kubectl delete pod winspike winspike-lowpriv --ignore-not-found --wait=false 2>&1 | Out-Null
    if (-not $KeepNode) {
        az aks nodepool scale -g $ResourceGroup --cluster-name $Cluster -n $WindowsPool --node-count 0 -o none
        Write-Host '  windows pool scaled back to 0'
    }
    else {
        Write-Host '  -KeepNode set: windows pool left at 1'
    }
}

if ($failed) { Write-Host "`nRESULT: FAIL" -ForegroundColor Red; exit 1 }
Write-Host "`nRESULT: PASS" -ForegroundColor Green
