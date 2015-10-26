FROM busybox:ubuntu-14.04

MAINTAINER Michael Stapelberg <michael+revoke@stapelberg.ch>

# So that we can run as unprivileged user inside the container.
RUN echo 'nobody:x:99:99:nobody:/:/bin/sh' >> /etc/passwd

USER nobody

ADD revoke /usr/bin/revoke

EXPOSE 8093

VOLUME ["/etc/revoke"]

ENTRYPOINT ["/usr/bin/revoke"]
