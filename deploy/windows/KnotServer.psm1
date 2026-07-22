Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

class KnotProcessRunner {
  [string]$RepositoryRoot

  KnotProcessRunner([string]$repositoryRoot) {
    $this.RepositoryRoot = $repositoryRoot
  }

  [void]Require([string]$command) {
    if ($null -eq (Get-Command $command -ErrorAction SilentlyContinue)) {
      throw "$command is required"
    }
  }

  [void]Run([string]$command, [string[]]$arguments) {
    Push-Location $this.RepositoryRoot
    try {
      & $command @arguments
      if ($LASTEXITCODE -ne 0) {
        throw "$command failed with exit code $LASTEXITCODE"
      }
    } finally {
      Pop-Location
    }
  }

  [string]Capture([string]$command, [string[]]$arguments) {
    Push-Location $this.RepositoryRoot
    try {
      $output = & $command @arguments 2>&1 | Out-String
      if ($LASTEXITCODE -ne 0) {
        throw "$command failed with exit code $LASTEXITCODE"
      }
      return $output.Trim()
    } finally {
      Pop-Location
    }
  }
}

class KnotTailscaleIdentity {
  [string]$DnsName

  KnotTailscaleIdentity([string]$dnsName) {
    $this.DnsName = $dnsName
  }

  static [KnotTailscaleIdentity]Discover([KnotProcessRunner]$runner) {
    $status = $runner.Capture("tailscale", @("status", "--json")) | ConvertFrom-Json
    $identityDnsName = [string]$status.Self.DNSName
    if ([string]::IsNullOrWhiteSpace($identityDnsName)) {
      throw "Tailscale MagicDNS is required"
    }
    return [KnotTailscaleIdentity]::new($identityDnsName.TrimEnd("."))
  }
}

class KnotEnvironmentFile {
  [string]$Path
  [System.Collections.Specialized.OrderedDictionary]$Values

  KnotEnvironmentFile([string]$path) {
    $this.Path = $path
    $this.Values = [ordered]@{}
    $this.Load()
  }

  [void]Configure([KnotTailscaleIdentity]$identity) {
    $serverOrigin = "https://$($identity.DnsName)"
    $objectsOrigin = "$serverOrigin`:8443"
    $this.EnsureSecret("KNOT_JWT_SECRET")
    $this.EnsureSecret("KNOT_PUSH_INTERNAL_TOKEN")
    $this.EnsureSecret("KNOT_INTERNAL_GRPC_TOKEN")
    $this.EnsureSecret("KNOT_DELIVERY_TOKEN_SECRET")
    $this.EnsureSecret("KNOT_POSTGRES_PASSWORD")
    $this.Set("KNOT_MINIO_ROOT_USER", "knotserver")
    $this.EnsureSecret("KNOT_MINIO_ROOT_PASSWORD")
    $this.Set("KNOT_BIND_ADDRESS", "127.0.0.1")
    $this.Set("KNOT_PUBLIC_SERVER_URL", $serverOrigin)
    $this.Set("KNOT_CORS_ORIGINS", $serverOrigin)
    $this.Set("KNOT_S3_PUBLIC_ENDPOINT", $objectsOrigin)
    $this.Set("KNOT_OBJECTS_ORIGIN", $objectsOrigin)
    $this.Set("KNOT_PUSH_PROVIDER_MODE", "disabled")
    $this.Save()
  }

  [string]ServerOrigin() {
    return [string]$this.Values["KNOT_PUBLIC_SERVER_URL"]
  }

  [string]ObjectsOrigin() {
    return [string]$this.Values["KNOT_S3_PUBLIC_ENDPOINT"]
  }

  hidden [void]Load() {
    if (-not (Test-Path $this.Path)) {
      return
    }
    foreach ($line in [IO.File]::ReadAllLines($this.Path)) {
      if ([string]::IsNullOrWhiteSpace($line)) {
        continue
      }
      $separator = $line.IndexOf("=")
      if ($separator -lt 1) {
        continue
      }
      $key = $line.Substring(0, $separator).Trim()
      $value = $line.Substring($separator + 1)
      $this.Values[$key] = $value
    }
  }

  hidden [void]Set([string]$key, [string]$value) {
    $this.Values[$key] = $value
  }

  hidden [void]EnsureSecret([string]$key) {
    if ($this.Values.Contains($key)) {
      $current = [string]$this.Values[$key]
      $insecure = (
        $current.StartsWith("development-only") -or
        $current.StartsWith("knot-development") -or
        $current.StartsWith("generate-a-") -or
        $current.Length -lt 32
      )
      if (-not $insecure) {
        return
      }
    }
    $bytes = New-Object byte[] 32
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
      $generator.GetBytes($bytes)
    } finally {
      $generator.Dispose()
    }
    $this.Values[$key] = ([BitConverter]::ToString($bytes)).Replace("-", "").ToLowerInvariant()
  }

  hidden [void]Save() {
    $lines = [Collections.Generic.List[string]]::new()
    foreach ($key in $this.Values.Keys) {
      $lines.Add("$key=$($this.Values[$key])")
    }
    $encoding = [Text.UTF8Encoding]::new($false)
    [IO.File]::WriteAllLines($this.Path, $lines, $encoding)
  }
}

class KnotTailscaleGateway {
  [KnotProcessRunner]$Runner

  KnotTailscaleGateway([KnotProcessRunner]$runner) {
    $this.Runner = $runner
  }

  [void]Configure() {
    $this.Runner.Run(
      "tailscale",
      @("serve", "--bg", "--https=443", "http://127.0.0.1:5173")
    )
    $this.Runner.Run(
      "tailscale",
      @("serve", "--bg", "--https=8443", "http://127.0.0.1:9000")
    )
  }
}

class KnotServerHealth {
  [KnotProcessRunner]$Runner

  KnotServerHealth([KnotProcessRunner]$runner) {
    $this.Runner = $runner
  }

  [void]Verify([KnotEnvironmentFile]$environment) {
    $this.Runner.Run("docker", @("compose", "ps"))
    $this.WaitFor("$($environment.ServerOrigin())/healthz")
    $this.WaitFor("$($environment.ObjectsOrigin())/minio/health/live")
  }

  hidden [void]WaitFor([string]$url) {
    for ($attempt = 1; $attempt -le 60; $attempt += 1) {
      try {
        $response = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 5
        if ($response.StatusCode -ge 200 -and $response.StatusCode -lt 300) {
          return
        }
      } catch {
        if ($attempt -eq 60) {
          throw "$url did not become healthy: $($_.Exception.Message)"
        }
      }
      Start-Sleep -Seconds 1
    }
  }
}

class KnotServerInstaller {
  [KnotProcessRunner]$Runner
  [KnotTailscaleGateway]$Gateway
  [KnotServerHealth]$Health

  KnotServerInstaller([string]$repositoryRoot) {
    $this.Runner = [KnotProcessRunner]::new($repositoryRoot)
    $this.Gateway = [KnotTailscaleGateway]::new($this.Runner)
    $this.Health = [KnotServerHealth]::new($this.Runner)
  }

  [void]Install() {
    $this.RequireRuntime()
    $identity = [KnotTailscaleIdentity]::Discover($this.Runner)
    $environment = [KnotEnvironmentFile]::new((Join-Path $this.Runner.RepositoryRoot ".env"))
    $environment.Configure($identity)
    $this.Runner.Run("docker", @("compose", "config", "--quiet"))
    $this.Runner.Run("docker", @("compose", "up", "--build", "-d"))
    $this.Gateway.Configure()
    $this.Health.Verify($environment)
    $this.PrintAddresses($environment)
  }

  [void]Start() {
    $this.RequireRuntime()
    $environment = [KnotEnvironmentFile]::new((Join-Path $this.Runner.RepositoryRoot ".env"))
    if ([string]::IsNullOrWhiteSpace($environment.ServerOrigin())) {
      throw "Run Install-KnotServer.ps1 first"
    }
    $this.Runner.Run("docker", @("compose", "up", "-d"))
    $this.Gateway.Configure()
    $this.Health.Verify($environment)
    $this.PrintAddresses($environment)
  }

  [void]Test() {
    $this.RequireRuntime()
    $environment = [KnotEnvironmentFile]::new((Join-Path $this.Runner.RepositoryRoot ".env"))
    if ([string]::IsNullOrWhiteSpace($environment.ServerOrigin())) {
      throw "Run Install-KnotServer.ps1 first"
    }
    $this.Health.Verify($environment)
    $this.PrintAddresses($environment)
  }

  hidden [void]RequireRuntime() {
    $this.Runner.Require("docker")
    $this.Runner.Require("tailscale")
    $this.Runner.Capture("docker", @("compose", "version")) | Out-Null
  }

  hidden [void]PrintAddresses([KnotEnvironmentFile]$environment) {
    [Console]::WriteLine("Knot Web: $($environment.ServerOrigin())")
    [Console]::WriteLine("Knot objects: $($environment.ObjectsOrigin())")
    [Console]::WriteLine("Apple build setting: KNOT_SERVER_BASE_URL=$($environment.ServerOrigin())")
  }
}

function Install-KnotServer {
  param([Parameter(Mandatory = $true)][string]$RepositoryRoot)
  [KnotServerInstaller]::new($RepositoryRoot).Install()
}

function Start-KnotServer {
  param([Parameter(Mandatory = $true)][string]$RepositoryRoot)
  [KnotServerInstaller]::new($RepositoryRoot).Start()
}

function Test-KnotServer {
  param([Parameter(Mandatory = $true)][string]$RepositoryRoot)
  [KnotServerInstaller]::new($RepositoryRoot).Test()
}

Export-ModuleMember -Function Install-KnotServer, Start-KnotServer, Test-KnotServer
