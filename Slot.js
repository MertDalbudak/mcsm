const fs = require('fs').promises;
const Event = require('events');
const {exec} = require('child_process');
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

        /**
         * @type {Server}
         */
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
                    await this.assignServer(data.id);
                    await this.server.init();
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
    async getServerStatus(host = 'localhost', port = this.config.localPort){
        let data = null;
        try{
            data = await McUtil.status(host, port, {'timeout': 500});
        }
        catch(error){
            if(this.server && this.status == "running"){
                this.server.die();
            }
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
                    if(this.status == "not running")
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
                this.event.emit('stopped');
                break;
        }
        if(this.server){
            this.server.die();
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
        console.log(this.server);
        
        this.server.slot = this;

        const port_assign = async ()=>{
            await this.server.setPort(this.config.localPort);
            this.event.emit('serverAssigned');
            this.getServerStatus();
        };
        if(this.server.status == 'init'){
            await new Promise((res)=>{
                this.server.on('ready', async ()=> {
                    await port_assign();
                    res();
                });
            })
        }
        else{
            await port_assign();
        }
    }

    async startServer(id){
        if(this.status == "restart" || this.status == "restarting" || this.status == "starting"){
            return "An start up or restart is already in process.";
        }
        
        if(this.server != null){
            this.status = "restart";
            this.event.emit('restart');
            try{
                await (new Promise((resolve, reject)=>{
                    this.stopServer((error, data)=>{
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
            this.suspendServerStart();  // SUSPEND SLOT SERVER START
            
            await this.assignServer(id);    // ASSIGN SERVER TO THIS SLOT
            // TRY STARTING THE SERVER

            console.log('starting');
            
            
            this.server.start();

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
            callback(null, {'message': "Server has been started."});
        }
        catch(error){
            callback("Current server have been stopped but something went wrong starting the desired server", {'message': error})
            console.error(error);
        }
    }

    stopServer(callback){
        this.server.stop((error)=>{
            if(error){
                callback(error, {'message': "An error occured"});
            }
            else{
                this.status = "not running";
                this.event.emit('stopped');
                callback(null, {'message': "Server has been stopped"});
            }
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