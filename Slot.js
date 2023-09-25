const fs = require('fs').promises;
const Event = require('events');
const {exec, spawn} = require('child_process');
const config = require('./config.json').Slot;
const util = require('minecraft-server-util');
const Server = require('./Server/Server');

class Slot{
    constructor(server_id){
        this.config = config;

        this.id = this.config.id;
        this.name = this.config.name;

        this.event = new Event();
        this.status = "not running";
        this.daemon_status = "not running";
        this.suspend_mc_start = false;

        this.server = null;

        if(server_id){
            this.assignServer(server_id);
        }
        else{
            // CHECK IF AN SERVER IS ALIVE IN SLOT
            this.getLiveData().then(async (data) => {
                if(data){
                    const id = await Server.findIdByMotd(data.motd.clean);
                    if(isNaN(id) == false){
                        this.assignServer(id);
                    }
                }
            });
        }
        this.event.emit('ready');
    }

    async getLiveData(){
        let data = null;
        try{
            data = await util.status('localhost', this.port, {'timeout': 800});
        }
        catch(error){
            console.log("No server is running currently")
            // console.error(error);
        }
        finally{
            return data;
        }
    }

    async assignServer(server_id){
        const server_config = (await Server.available_servers).find(e => e.id == server_id);
        if(server_config == undefined){
            throw new Error(`No server with given id ${server_id} found.`);
        }
        const _Server = require(`./Server/${server_config.type}`);
        this.server = new _Server(server_id);
        this.server.slot = this;
        await (new Promise((res) => {
            this.server.on('ready', async ()=>{
                await this.server.setPort(this.port);
                this.status = "running";
                this.event.emit('serverAssigned', this.server);
                this.checkDaemon();
                res();
            });
        }));
    }

    checkDaemon(callback){
        exec(`if screen -ls | grep -q '${this.server.bin}'; then echo 1; else echo 0; fi`,  (err, stdout, stderr)=>{
            console.log("Server Manager Status: " + this.status);
            if(stdout == 0){
                this.daemon_status = "not running";
                if(this.status == "running"){
                    this.status = "not running";
                    //console.error("Minecraft server is not running anymore", "Exiting...");
                    //process.exit();
                }
                else{
                    switch(this.status){
                        case "restarting":
                            this.status = "restart closed";
                            this.event.emit("restartClosed");
                            break;
                        case "updating":
                            this.status = "update downloading";
                            this.event.emit("updateClosed");
                            break;
                    }
                }
            }
            else{
                this.daemon_status = "running";
                switch(this.status){
                    case "not running":
                        this.status = "running";
                        break;
                    case "starting":
                        this.event.emit('serverStarted');
                        this.status = "running";
                        break;
                    case "restart closed":
                        this.event.emit("restartComplete");
                        this.status = "running";
                        break;
                    case "update installed":
                        this.event.emit("updateComplete");
                        this.status = "running";
                        break;
                }
            }
            if(callback){
                callback(stdout != 0);
            }
        });

    }
    async startServer(id, callback){
        if(this.status == "restart" || this.status == "restarting" || this.status == "starting"){
            callback("An start up or restart is already in process.", {})
            return;
        }
        const spawn_options = {
            slient: true,
            detached: true,
            stdio: 'ignore',
            shell: false,
            windowsHide: true
        }
        if(this.server != null){
            this.status = "restart";
            this.event.emit('restart');
            try{
                await (new Promise((resolve, reject)=>{
                    this.stopServer((error, data)=>{
                        if(this.server.discord){
                            this.server.discord.send(`The Minecraft server thats linked to this channel stopped`);
                        }
                        if(error){
                            reject([error, data]);
                        }
                        else{
                            resolve();
                        }
                    });
                }));
            }
            catch(error){
                callback(...error);
                console.error(error);
            }
            this.server = null;
        }
        try{
            // SET NEW STATUS
            switch(this.status){
                case "restarting":
                    this.event.emit("restarted");
                    break;
                case "not running":
                default:
                    this.event.emit("started");
                    break;
            }
            this.suspendServerStart();  // SUSPEND SLOT SERVER START
            await this.assignServer(id);    // ASSIGN SERVER TO THIS SLOT
            // TRY STARTING THE SERVER
            const mc_server_spawn = spawn('bash', [`${process.env.ROOT}/bin/start`, `-p ${this.server.path}`], spawn_options);
            mc_server_spawn.unref();
            this.server.discord.on('ready', ()=>{
                this.server.discord.send(`The Minecraft server thats linked to this channel has started. You might be able to join the server in a minute.`);
            });
            
            callback(null, {'message': "Server has been started."});
        }
        catch(error){
            callback("Current server have been stopped but something went wrong starting the desired server", {'message': error})
            console.error(error);
        }
    }

    stopServer(callback){
        this.server.handler.say("Server will stop");
        this.server.handler.stop(0, false, false, false).then(()=>{
            if(this.server.discord){
                this.server.discord.send(`The Minecraft server thats linked to this channel will be stopped`).then(()=>{
                    this.server.die();
                    this.server = null;
                });
            }
            else{
                this.server = null;
            }
            switch(this.status){
                case "restart":
                    this.status = "restarting";
                    this.event.emit("restarting");
                    break;
                case "running":
                default:
                    this.status = "not running";
                    break;
            }
            callback(null, {'message': "Server has been stopped"});
        }, (error)=>{
            callback(error, {'message': "An error occured"});
        });
    }

    suspendServerStart(){
        if(suspend_server_start == false){
            suspend_server_start = true;
            setTimeout(() => {
                suspend_server_start = false;
            }, 3 * 60 * 1000);
        }
    }

    on(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.on(name, callback);
    }
    once(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.once(name, callback);
    }

    // GETTER
    get ops() {
        return require(`${this.server.path}/ops.json`).map(e => e.name);
    }
    get daemon_status (){
        return daemon_status;
    }
    async report(){
        let server = null;
        if(this.server){
            server = await this.getLiveData();
            if(server != null){
                server.id = this.server.id;
            }
        }
        return {
            'id': this.id,
            'name': this.name,
            'domain': this.config.domain,
            'port': this.config.port,
            'srvRecord': this.config.srvRecord,
            'available_server': (await Server.getAvailableServers()) || [],
            'status': this.status,
            'server': server
        }
    }
    // SETTER
    set daemon_status(status){
        daemon_status = status;
        this.event.emit('daemonStatusChange', status);
    }
}

// PRIVATE VAR
let daemon_status = "";
let suspend_server_start = false;

module.exports = Slot;