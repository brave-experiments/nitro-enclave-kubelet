FROM rust:1.70 as eif_utils

RUN cargo install --git https://github.com/aws/aws-nitro-enclaves-image-format --example eif_build

FROM golang:1.20 as kubelet

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /build ./cmd/build
RUN CGO_ENABLED=0 GOOS=linux go build -o /shell ./cmd/shell
RUN CGO_ENABLED=0 GOOS=linux go build -o /vk ./cmd

FROM amazonlinux:2.0.20230207.0

RUN amazon-linux-extras install aws-nitro-enclaves-cli -y && \
    yum install aws-nitro-enclaves-cli-devel git openssl11 wget patchelf awscli -y && \
    rpm -e --nodeps docker && \
    yum clean all && \
    rm -rf /var/cache/yum

RUN mkdir /opt/glibc-2.28 && \
    cd /opt/glibc-2.28 && \
    wget https://vault.centos.org/centos/8/BaseOS/x86_64/os/Packages/glibc-2.28-164.el8.x86_64.rpm && \
    rpm2cpio glibc-2.28-164.el8.x86_64.rpm | cpio -idmv && \
    rm glibc-2.28-164.el8.x86_64.rpm 

COPY --from=eif_utils /usr/local/cargo/bin/eif_build /bin/eif_build 
RUN patchelf --set-interpreter /opt/glibc-2.28/lib64/ld-linux-x86-64.so.2 --set-rpath /opt/glibc-2.28/lib64:/usr/lib64 /bin/eif_build
COPY --from=kubelet /build /bin/build
COPY --from=kubelet /shell /bin/shell
COPY --from=kubelet /vk /bin/vk
