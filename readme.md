# Cisco ssh client

This app is a wrapper for **networklore/netrasp** package, which provides a convenient way to run commands to network devices via SSH.
Though netrasp gives ability to work with other vendor's devices, this app is focused on cisco IOS-like commands (because of command error handlering).
Written in go.

___
## Usage
Typical folder structure for app to run is below

```
.
└── app_root_dir/
    ├── app_executable
    ├── config/
    │   └── config.yml
    ├── input/
    │   ├── devices.csv
    │   ├── commandsFile_1
    │   ├── ...
    │   └── commandsFile_N
    └── output/
        ├── results.txt
        ├── device1_commandStatus.txt
        ├── ...
        └── device1_commandStatus.txt
```
App reads config file from ./config/config.yml file, finds there input data (devices parameters, files with commands, etc.), 
runs the commands specified, and puts output from each device to corresponding file located in "output" directory. Finally, it stores the summary
status to "output/results.txt"

### Input data format
___
**config.yml** parameters meaning is below:

| Option | Description |
| ------ | ----------- |
| client, ssh_timeout   | SSH timeout value (in seconds), used for all devices |
| data, input_folder | Directory where devices info and command files are located |
| data, devices_data | Filename for csv-formatted devices file (must be inside "intput_folder" directory) |
| data, output_folder | Directory where outputs are stored |
| data, results_data | Filename for summary status (will be placed to "output_folder") |

config.yml example:
```yaml
client:
  ssh_timeout: 2
data:
  input_folder: "./input/"
  devices_data: "devices.csv"
  output_folder: "./output/"
  results_data: "results.txt"
```
___
**devices data file** is csv file, it must contain the following fields
```
hostname,login,password,osType,configure,cmdFile
1.1.1.1,admin,password,ios,true,router_commands.txt
r02.testenv.demo,admin,password,ios,true,router_commands.txt
1.1.1.3,admin,password,ios,false,switch_commands.txt
```
The fileds meaning:
| Option | Description |
| ------ | ----------- |
| hostname | device ip/fqdn |
| login | ssh login |
| password | ssh password |
| osType | "ios" for cisco, other platform values specified by **networklore/netrasp** are allowed |
| configure | whether we want to issue config commands, boolean true/false |
| cmdFile | filename with actual commands |

In example above, we want to run config commands from *router_commands.txt* file on two devices: 1.1.1.1 and r02.testenv.demo,
and non-config commands from *switch_commands.txt* on single device 1.1.1.3
___
**Files with commands** are simple text files, each line represent command we want to issue, for example:
```
show ip route
sh ip int br
```
or 
```
int lo10
vrf forw TEST
ip addr 1.1.1.1 255.255.255.0
no shut
```
___
### Output data format

For configure option, the output for example above will be like this:
```
======================== "15 Sep 23 19:11 EEST" =======================
device: "1.1.1.1", command: "int lo10", accepted: true, error: ""
device: "1.1.1.1", command: "vrf forw TEST", accepted: true, error: ""
device: "1.1.1.1", command: "ip addr 1.1.1.1 255.255.255.0", accepted: true, error: ""
device: "1.1.1.1", command: "no shut", accepted: true, error: ""
```
If command is incorrect, will get the following:
```
======================== "15 Sep 23 19:11 EEST" =======================
device: "1.1.1.1", command: "asdf", accepted: false, error: "% Invalid input detected at '^' marker."
```
If we chose configure = false option in devices csv file, the output will look like:
```
======================== "15 Sep 23 19:25 EEST" =======================
device: "1.1.1.3", command: "sh int status", accepted: true, error: "" output:

Port         Name               Status       Vlan       Duplex  Speed Type
Gi1/0/1                         connected    1000       a-full a-1000 10/100/1000BaseTX
Gi1/0/2      -=Printer RICON=-  connected    4            full    100 10/100/1000BaseTX
```
If command is incorrect, will get the following:
```
======================== "15 Sep 23 19:24 EEST" =======================
device: "1.1.1.3", command: "asdf", accepted: false, error: "% Invalid input detected at '^' marker."
```
The summary output file looks like this:
```
+------------------+---------+-----------+-------------------------------+
|      DEVICE      | OS TYPE | CONFIGURE |      COMMAND RUN STATUS       |
+------------------+---------+-----------+-------------------------------+
| 1.1.1.1          | ios     | false     | Success                       |
| 1.1.1.3          | ios     | true      | Commands accepted with errors |
| r02.testenv.demo | ios     | true      | Unreachable                   |
+------------------+---------+-----------+-------------------------------+
|                                              15 SEP 23 19:29 EEST      |
+------------------+---------+-----------+-------------------------------+
```
## Issues
Currently commands which contains '+' char makes app hangs, for example "aaa group server tacacs+ ISE". The issue is reported to **networklore/netrasp**