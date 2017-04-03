#!/bin/sh

cd `dirname $0`

project=api-pantahub-com

if test -n "$GCLOUD_PROJECT"; then
	project=$GCLOUD_PROJECT
fi

gcloud app deploy --project "$project"

