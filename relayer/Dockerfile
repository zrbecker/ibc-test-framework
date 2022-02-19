FROM ubuntu

RUN apt update -y
RUN apt install git wget make -y

RUN wget https://go.dev/dl/go1.17.7.linux-amd64.tar.gz
RUN tar -xzf go1.17.7.linux-amd64.tar.gz -C /usr/local
RUN ln -s /usr/local/go/bin/go /usr/local/bin/go

RUN git clone https://github.com/strangelove-ventures/relayer.git
RUN make -C relayer install

RUN cp /root/go/bin/rly /usr/local/bin/rly
