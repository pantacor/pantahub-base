path=$1

openssl genrsa -aes256 -out  $path.tmp 2048
openssl rsa -in $path.tmp -out $path

