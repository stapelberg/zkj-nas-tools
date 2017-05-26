FROM debian:jessie
MAINTAINER Michael Stapelberg <michael+nas@stapelberg.ch>

RUN apt-get update \
    && apt-get install -y rsync ssh

ADD sync.pl /usr/bin/

ENTRYPOINT ["/usr/bin/sync.pl"]
