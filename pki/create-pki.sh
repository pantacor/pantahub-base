set -x

path=$1
sh root-config.sh $path

cd $path
openssl genrsa -aes256 -out private/ca.key.pem 4096

chmod 400 private/ca.key.pem

openssl req -config openssl.cnf \
      -key private/ca.key.pem \
      -new -x509 -days 7300 -sha256 -extensions v3_ca \
      -out certs/ca.cert.pem

echo
echo "Verifying cert"

openssl x509 -noout -text -in certs/ca.cert.pem
