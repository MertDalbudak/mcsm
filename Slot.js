const fs = require('fs').promises;
const Event = require('events');
const {exec, spawn} = require('child_process');
const util = require('node:util');
exec.__promise__ = util.promisify(exec);
const config = require('./config.json').Slot;
const McUtil = require('minecraft-server-util');
const Server = require('./Server/Server');
const path = require('path');

class Slot{
    constructor(server_id){
        this.config = config;

        this.id = this.config.id;
        this.name = this.config.name;

        this.event = new Event();
        this.status = "not running";
        this.suspend_mc_start = false;

        this.server = null;
        if(server_id){
            this.startServer(server_id, (error, response)=>{
                if(error){
                    console.error(error.toString());
                }
                this.event.emit('ready');
            });
        }
        else{
            // CHECK IF AN SERVER IS ALIVE IN SLOT
            this.getLiveServer().then(async (data) => {
                if(data != null){
                    this.assignServer(data.id);
                }
                this.event.emit('ready');
            });
        }
    }

    /**
     * 
     * @param {String} host 
     * @param {Number} port 
     * @returns
     */
    async getServerStatus(host = 'localhost', port = this.config.port){
        let data = null;
        try{
            data = await McUtil.status(host, port, {'timeout': 500});
        }
        catch(error){
            console.log("No server is running currently")
            // console.error(error);
        }
        finally{
            return data;
        }
    }

    /**
     * @returns {ServerData|null}
     */
    async getLiveServer(){
        let available_servers = await Server.available_servers;
        for(let i = 0; i < available_servers.length; i++){
            try{
                const level_name = available_servers[i].properties.match(/level-name=(.)+/g)[0].split('=')[1].trim();
                const session_lock_file = path.join(available_servers[i].path, level_name, 'session.lock');
                const {stderr, stdout} = await exec.__promise__(`lsof -n ${session_lock_file}`);
                if(stderr == "" && stdout != ""){
                    this.event.emit('started');
                    return available_servers[i];
                }
            }
            catch(error){
                console.error(error.toString());
            }
        }
        switch(this.status){
            case "running":
                this.server.die();
                this.event.emit('stopped');
                break;
        }
        this.status = "not running";
        return null;
    }

    async assignServer(server_id){
        const server_data = (await Server.available_servers).find(e => e.id == server_id);
        if(server_data == undefined){
            throw new Error(`No server with given id ${server_id} found.`);
        }
        const _Server = require(`./Server/${server_data.type}`);
        this.server = new _Server(server_id);
        this.server.slot = this;
        await (new Promise((res) => {
            this.server.on('ready', async ()=>{
                await this.server.setPort(this.config.port);
                this.status = "running";
                this.event.emit('serverAssigned', this.server);
                this.getServerStatus();
                res();
            });

        }));
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
            const mc_server_spawn = spawn('sh', [`${process.env.ROOT}/bin/start.sh`, `-p ${this.server.path}`], spawn_options);
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
    async report(){
        let server = null;
        if(this.server){
            server = await this.getServerStatus();
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
            'available_servers': (await Server.getAvailableServers()) || [],
            'status': this.status,
            'server': server
        }
    }
    // SETTER
}

// PRIVATE VAR
let suspend_server_start = false;

module.exports = Slot;