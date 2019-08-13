# Serf Bandwidth Testing

This repo is mainly to test out how the network bandwidth required by a [Serf](https://github.com/hashicorp/serf) "server" increases with the number of distinct Serf "pools" it runs when the total number of "clients" connected to all the pools remains constant.

## What Gets Run

The docker-compose files have a number of services:

* `server1`, `server2` and `server3` - These are the "server" Serf instances that will run 1, 2 or 4 Serf "pools". Each client will get connected to exactly one of the pools and there is no bleeding of membership data between them.
* `client1` - Serf client that will connect to the first pool of the servers.
* `client2` - Serf client that will connect to the second pool of the servers.
* `client3` - Serf client that will connect to the third pool of the servers.
* `client4` - Serf client that will connect to the fourth pool of the servers.
* `influxdb` - Runs influxdb for metric storage and querying
* `telegraf` - This is configured to pull docker metrics and push them to influxdb
* `chronograf` - This is for visualization of the metrics in influxdb

It is intended that the `client*` services be scaled with the `--scale` command line argument to `docker-compose up`.

## How To Run

### One Serf Pool

```bash
export COMPOSE_PROJECT_NAME=serf
export COMPOSE_FILE=docker-compose.yml

docker-compose up -d --scale client1=100 server1 server2 server3 client1
```

### Two Serf Pools
```bash
export COMPOSE_PROJECT_NAME=serf
export COMPOSE_FILE=docker-compose.yml:serf-2.yml

docker-compose up -d --scale client1=50 --scale client2=50 server1 server2 server3 client1 client2
```

#### Four Serf Pools

```bash
export COMPOSE_PROJECT_NAME=serf
export COMPOSE_FILE=docker-compose.yml:serf-4.yml

docker-compose up -d --scale client1=25 --scale client2=25 --scale client3=25 --scale client4=25 server1 server2 server3 client1 client2 client3 client4
```

## Visualization

The chronograf ui is port mapped to http://localhost:8888. You can import the dashboard from the serf_testing_dashboard.json file in this repo for my default display.

