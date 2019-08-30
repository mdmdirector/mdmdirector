FROM golang:1.12

LABEL "com.github.actions.name"="github-action-publish-binaries"
LABEL "com.github.actions.description"="Upload artifacts when new releases are made"
LABEL "com.github.actions.icon"="save"
LABEL "com.github.actions.color"="gray-dark"


RUN apt-get update
RUN apt-get install --yes \
  ca-certificates \
  curl \
  jq

COPY upload-script /usr/bin/upload-script
RUN chmod 755  /usr/bin/upload-script

ENTRYPOINT ["upload-script"]
