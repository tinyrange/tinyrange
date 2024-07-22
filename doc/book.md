# TinyRange Documentation

TinyRange is a light-weight scriptable orchestration system for building and running virtual machines with a focus on speed and flexibility for development.

TinyRange is split into two major components. The Build System (also called `pkg2`) and the Executor called `tinyrange`. 

At a high level this pair operates like a compiler and a virtual machine. The build system compiles a virtual machine definition made from filesystem fragments and intermediate build steps then TinyRange constructs a root filesystem and boots the virtual machine.

## Build System Documentation

The build system consumes definitions and writes build outputs to the local filesystem. Build outputs are named according to the hash of the build definition.

The build system is highly scriptable in a language called Starlark. Starlark has a syntax similar to Python but it's deeply integrated with the build system core code and highly parallelizable.

### Example

Let's examine the build system by looking at how it makes a simple Alpine Linux virtual machine with the `gcc` package installed.

The top level definition is `define.build_vm`. This definition runs a virtual machine given a series of directives and writes a file from the virtual machine as a output. The output portion is not used here and the virtual machine is unconditionally rebuilt since the user is expected to interact with it directly.

#### Directives

As a brief aside a directive is a instruction used for building the virtual machine. At a low level directives are things like load this archive from disk, put this file into the filesystem, or copy this builtin file to this path inside the guest. TinyRange (the executor) understands those directives directly. At a higher level we have directives like run a command. In addition most build definitions can be interpreted as directives depending on their output. As a simple example a downloaded file can be copied nto a virtual machine using a file directive.

The two directives we use in our example are `define.plan` and `directive.run_command`. The latter just tells the virtual machine to run a interactive shell when it boots. The former is the part that lists the required packages and turns into the series of directives representing the installed system.

#### Planner

Internally `define.plan` wraps the container builder and installation planning systems. It defines a builder which is one of the possible container builder, a series of queries to look for packages, and finally a list of tags used to customize the build process.

##### Planner Tags

The container builders in TinyRange use a common system of tags to represent how packages are installed.

- **Level 1**: Use the system package manager and a existing docker container to install packages according to their names.
- **Level 2**: The same as **Level 1** except it resolves package dependencies and installs dependencies first.
- **Level 3** This runs independently of the system package manager and uses the build system to download and "install" packages into archives.

We also define some theoretical additional levels which we hope to support in the future.

- **Level 4** Like **Level 3** except the packages are built from scratch using the published package build scripts.
- **Level 5** Like **Level 4** except the build is completely controlled by TinyRange using extracted metadata from published or inferred build scripts.

Here we will be performing a build using **Level 3** so the build system will be in charge of the installation.

#### Container Builder

The container builder is constructed and registered in a Starlark script. A container builder contains a package collection to generate a installation plan and a callback that receives a generated install plan and returns a runnable set of directives.

#### Package Collection

The package collection uses multiple stages to load package metadata and create "installers". The first stage downloads the raw package metadata and converts it into a record file (A optimized form of JSON with one record per line). The second stage pulls the basic metadata of packages including their names and aliases. The third stage requests a installer given a tag list.

##### First Stage: Metadata Collection

The first stage is to convert the metadata into a record file. The metadata comes in various formats depending on the package manager. While Alpine uses a simple series of key value pairs RPM uses XML for example. This first stage is a regular build defined in Starlark. It can take any arguments and it can use sub-builds to download required files or perform other conversions.

In the case of Alpine this build downloads and extracts each `APKINDEX.tar.gz` file and parses the records into a internal JSON format. This format is exported as a record file.

##### Second Stage: Metadata Transformation

The second stage is a specialized callback that takes a list of callbacks and converts them into packages. This is the stage that actually loads the packages into the in-memory index.

Multiple callbacks run in parallel on different threads to further optimize package loading speed.

Packages can have more than one name when a package provides for certain meta-packages. This information needs to be known when packages are searched for so the second stage needs to collect this information from the record.

##### Third Stage: Installer Selection

The final stage knows which packages are valid options matching the query and uses another callback to select a installer. Different installers can be used depending on package metadata or the build tags specified.

Installers contain a list of directives and a series of dependencies. Dependencies are additional package queries which are ordered before the directives specified in the installer.

In this case a **Level 3** installer is selected which specifies another build definition as the installed form of the package.

#### Container Builder Callback

Once a installation plan is generated it is passed to the container builder callback. For **Level 1** or **Level 2** plans this might download a OCI image to act as the base but for **Level 3** it may do nothing or provide a final step with system configuration and also run package build scripts.

Once a list of directives are determined they are compiled into the low level directives understood by TinyRange. This step triggers the installation. The package is not really installed at this stage but rather converted from it's native format into a Archive that can be used inside TinyRange.

Alpine packages are distributed as `.apk` files. These are just `.tar.gz` files with additional files in the root which are not installed into the machine. These additional files include a copy of the package metadata (so packages can be installed without using a repository), signatures, and build scripts. Since all these files are identically named the conversion process puts them into package specific sub directories.

The Alpine plan callback also adds a additional directive with all the other directives passed to it. This final directive finds pre/post install scripts and generates a archive that runs those scripts.

#### Final Steps

Now that the plan is generated it can be turned into a TinyRange configuration file and booted. All the packages are "installed" before the VM boots and the installation scripts are triggered as the VM boots up.

## TinyRange Documentation

`TODO(joshua)`