$timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
$outFile = "backups/dashboard_$timestamp.sql"

docker exec gta-mysql mysqldump -uroot -pfuckThisp4s40Rd dashboard > $outFile

if ($LASTEXITCODE -ne 0 -or (Get-Item $outFile).Length -eq 0) {
    Write-Host "BACKUP FAILED - check MySQL credentials or container status" -ForegroundColor Red
    Remove-Item $outFile -ErrorAction SilentlyContinue
    exit 1
}

$backups = Get-ChildItem backups -Filter "dashboard_*.sql" | Sort-Object LastWriteTime -Descending
if ($backups.Count -gt 7) {
    $backups | Select-Object -Skip 7 | Remove-Item
    Write-Host "Pruned old backups, keeping the 7 most recent."
}

Write-Host "Backup complete: dashboard_$timestamp.sql" -ForegroundColor Green
