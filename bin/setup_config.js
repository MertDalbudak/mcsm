let p = require('path');
let fs = require('fs');
const readline = require('readline');
const { execSync } = require('child_process');

const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout
});

const config_path = p.resolve(`${__dirname}/../config.json`);
const config_default_path = p.resolve(`${__dirname}/../resources/config-default.json`);
const server_config_default_path = p.resolve(`${__dirname}/../resources/server-default-config.json`);

let config_default, config;

// CHECK IF config.json EXISTS
fs.access(config_path, async(error) => {
    if(error){
        // COPY config-default.json
        console.log('Reading config-default.json');
        config_default = require(config_default_path);
        config = config_default;
        await config_questionaire();
    }
    else {
        rl.close();
        config = require(config_path);
        console.log(`Config file already exists.`)
    }
    server_plugin_setup();
});

function server_plugin_setup(){
    // CHECK IF PLUGIN ALREADY INSTALLED
    for(let i = 0; i < config.Server.path.length; i++){
        let path = config.Server.path[i];
        try{
            fs.readdirSync(path, { withFileTypes: true }).forEach(server_dir => {
                if(!server_dir.isDirectory()){
                    return;
                }
                let server_dirname = server_dir.name;
                let server_path = `${path}/${server_dirname}`
                let plugin_path = `${server_path}/plugins/mcsm`;
                let plugin_config = `${plugin_path}/config.json`;
                fs.access(plugin_path, (error)=>{
                    if(error){
                        console.log('Server plugin not installed. Installing...')
                        try{
                            execSync(`sudo chown -R mcsm:mcsm ${server_path}`);
                            execSync(`sudo chmod -R 777 ${server_path}`);
                            fs.mkdirSync(plugin_path);
                        }
                        catch(error){
                            console.error(`Cannot create mcsm plugin folder for ${server_path}`);
                            return;
                        }
                    }
                    fs.access(plugin_config, (error)=>{
                        if(error){
                            let server_config = JSON.parse(fs.readFileSync(server_config_default_path));
                            server_config.id = parseInt(Math.random().toFixed(16).toString().substring(2)); // RANDOM NUMBER
                            fs.writeFileSync(plugin_config, JSON.stringify(server_config, null, "\t"));
                            try{
                                execSync(`sudo chown mcsm:mcsm ${plugin_config}`);
                                execSync(`sudo chmod 777 ${plugin_config}`);
                            }
                            catch(error){
                                console.error(`Couldn't set correct permissions to ${plugin_config}`);
                                return;
                            }
                        }
                    });
                });
            });
        }
        catch(error){
            console.log(`An error occured reading contents of ${path}`);
            console.error(error);
        }
    }
}

async function config_questionaire(){
    await ask(slot_id);
    await ask(slot_name);
    await ask(slot_host);
    await ask(slot_port);
    await ask(slot_srv);
    await ask(local_port);
    await ask(backup_path);
    await ask(web_api);
    await ask(server_log_check_interval);
    await ask(server_path);

    rl.close();
    fs.writeFileSync(config_path, JSON.stringify(config, null, "\t"));

    console.log("Config file successfully created. For more options edit config.js manually");
}

async function ask(question){
    let error = null;
    do {
        try{
            await question();
            error = null;
        }catch(err){
            error = err;
            console.error(err);
        }
    }while(error != null);
}

const slot_id = () => new Promise((res, rej) => {
    rl.question(`Specify a slot id (default: ${config_default.Slot.id}): `, id =>{
        if(isNaN(id)){
            rej("Input must be numeric");
        }
        else{
            config.Slot.id = parseInt(id) || config_default.Slot.id;
            res();
        }
    });
});

const slot_name = () => new Promise((res, rej) => {
    rl.question(`Specify a slot name for your mcsm server: `, name =>{
        if(name == null || name == ""){
            rej("Name cannot be empty");
        }
        else {
            config.Slot.name = name;
            res();
        }
    });
});

const slot_host = () => new Promise((res, rej) => {
    rl.question(`Enter the domain of your server (e.g.: example.com): `, domain =>{
        if(domain == null || domain == ""){
            rej("Domain cannot be empty");
        }
        else {
            config.Slot.domain = domain;
            res();
        }
    });
});

const slot_port = () => new Promise((res, rej) => {
    rl.question(`Specify a slot port which will be exposed (default: ${config_default.Slot.port}): `, port =>{
        if(isNaN(port)){
            rej("Input must be numeric");
        }
        else{
            config.Slot.port = parseInt(port) || config_default.Slot.port;
            res();
        }
    });
});

const slot_srv = () => new Promise((res, rej) => {
    rl.question(`SRV Record present? (y/n): `, answer =>{
        switch(answer){
            case 'y':
            case 'Y':
                config.Slot.srvRecord = true;
                break;
            case 'n':
            case 'N':
                config.Slot.srvRecord = false;
                break;
            default:
                rej("Invalid answer");
                return;
        }
        res();
    });
});

const local_port = () => new Promise((res, rej) => {
    rl.question(`Specify the local port (default: ${config_default.Slot.localPort}): `, port =>{
        if(isNaN(port)){
            rej("Input must be numeric");
        }
        else{
            config.Slot.localPort = parseInt(port) || config_default.Slot.localPort;
            res();
        }
    });
});

const backup_path = () => (new Promise((res, rej) => {
    rl.question("Specify a backup path: ", path =>{
        config.BackupPath = p.resolve(path);
        fs.access(config.BackupPath, (error) => {
            if(error) {
                try {
                    console.log("Given backup directory does not exist. Trying to create backup directory...");
                    fs.mkdirSync(config.BackupPath);
                    console.log("Backup directory created");
                    res();
                }
                catch (err){
                    rej(error);
                }
            }
            else{
                res();
            }
        })
    });
}));

const web_api = () => new Promise((res, rej)=>{
    rl.question(`Do you want to activate the web api? (y/n): `, async answer => {
        switch(answer){
            case 'y':
            case 'Y':
                config.WebAPI = true;
                await ask(api_port);
                await ask(api_auth);
                break;
            case 'n':
            case 'N':
                config.WebAPI = false;
                break;
            default:
                rej("Invalid answer");
                return;
        }
        res();
    });
})

const api_port = () => new Promise((res, rej) => {
    rl.question(`Specify mcsm api port (default: ${config_default.MCSM_API_PORT}): `, port =>{
        if(isNaN(port)){
            rej("Input must be numeric");
        }
        else{
            config.MCSM_API_PORT = parseInt(port) || config_default.MCSM_API_PORT;
            res();
        }
    });
});

const api_auth = () => new Promise((res, rej) => {
    rl.question(`Enter your mcsw API authentification token: `, token =>{
        if(token.match("^[a-fA-F0-9]{64}$")){
            config.MCSM_API_AUTHENTICATION = token;
            res();
        }
        else{
            rej("Authentification token is not vaild");
        }
    })
});

const server_log_check_interval = () => new Promise((res, rej) => {
    let default_value = config_default.Server.logCheckInterval;
    rl.question(`Specify a time interval for checking server logs in ms (default: ${default_value}): `, interval =>{
        if(isNaN(interval)){
            rej("Input must be numeric");
        }
        else{
            config.Server.logCheckInterval = parseInt(interval) || default_value;
            res();
        }
    });
});

const server_path = () => new Promise((res, rej) => {
    rl.question(`Specify the parent directory where your servers will be located: (e.g: /mnt/servers): `, path =>{
        if(config.Server.path.find(e => e == p.resolve(path))){
            rej("Server path already specified");
        }
        else{
            fs.access(path, (error) => {
                if(error) {
                    try {
                        console.log("Given server directory does not exist. Trying to create this directory.");
                        fs.mkdirSync(path);
                        console.log("Server directory created");
                    }
                    catch (err){
                        rej(error);
                        return;
                    }
                }
                config.Server.path.push(p.resolve(path));
                rl.question(`Do you want to specify another server directory (y/n): `, async (answer) => {
                    if(answer == 'Y' || answer == 'y'){
                        await ask(server_path);
                    }
                    res();
                });
            });
        }
    });
});