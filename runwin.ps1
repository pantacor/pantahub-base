#
# this script sources the env file, the gets, builds and starts pantahub
# using powershell. Enjoy!
#
$a = cat .\env| Select-String -Pattern '^[A-Za-z_]*='

foreach ($line in $a) {
  $str = $line.ToString()
  $strarr = $str.Split("=",2 )
  if ($strarr.Length -ne 2) {
    continue
  }
  $key = $strarr[0]
  $val = $strarr[1]
  [System.Environment]::SetEnvironmentVariable($key, $val)  
}

go get -u 2>&1 | %{ "$_" }
go build 2>&1 | %{ "$_" }
.\pantahub-base 2>&1 | %{ "$_" }
