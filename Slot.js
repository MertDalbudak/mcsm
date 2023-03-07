const fs = require('fs').promises;
const Event = require('events');
const {exec, spawn} = require('child_process');
const config = require('./config.json')
const util = require('minecraft-server-util');

class Slot{
    constructor(id, server_id){
        Object.assign(this, config.Slots.find(slot => slot.id == id));
        if(isNaN(this.id)){
            throw new Error(`No server with given id ${id} found.`);
        }
        this.event = new Event();
        this.status = "not running";
        this.daemon_status = "not running";
        this.suspend_mc_start = false;

        this.Server = null;

        if(isNaN(server_id) == false){
            this.assignServer(server_id);
        }
        else{
            // CHECK IF AN SERVER IS ALIVE IN SLOT
            this.getServerData().then(async (data) => {
                console.log(data);
                if(data){
                    const id = await Slot.findIdByMotd(data.motd.clean);
                    if(isNaN(id) == false){
                        this.assignServer(id);
                    }
                }
            });
        }
    }

    async getServerData(){
        let data = null;
        try{
            data = await util.status('localhost', this.port, {'timeout': 800});
        }
        catch(error){
            console.error(error);
        }
        finally{
            return data;
        }
    }

    assignServer(server_id){
        const server_config = config.Servers.find(e => e.id == server_id);
        if(server_config == undefined){
            throw new Error(`No server with given id ${id} found.`);
        }
        const Server = require(`./Server/${server_config.type}`);
        this.Server = new Server(server_id);
        this.status = "running";
        this.event.emit('serverAssigned', this.Server);
        this.checkDaemon();
    }

    checkDaemon(callback){
        exec(`if screen -ls | grep -q '${this.Server.bin}'; then echo 1; else echo 0; fi`,  (err, stdout, stderr)=>{
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
        if(this.Server != null){
            this.status = "restart";
            this.event.emit('restart');
            try{
                await (new Promise((resolve, reject)=>{
                    this.stopServer((error, data)=>{
                        if(this.Server.discord){
                            this.Server.discord.send(`The Minecraft server thats linked to this channel stopped`);
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
            this.Server = null;
        }
        try{
            switch(this.status){
                case "restarting":
                    this.event.emit("restarted");
                    break;
                case "not running":
                default:
                    this.event.emit("started");
                    break;
            }
            this.suspendServerStart();
            this.assignServer(id);
            const mc_server_spawn = spawn('bash', [this.Server.config.bin, ...this.Server.config.binOptions], spawn_options);
            mc_server_spawn.unref();
            this.Server.discord.on('ready', ()=>{
                this.Server.discord.send(`The Minecraft server thats linked to this channel has started. You might be able to join the server in a minute.`);
            });
            
            callback(null, {'message': "Server has been started."});
        }
        catch(error){
            callback("Current server have been stopped but something went wrong starting the desired server", {'message': error})
            console.log(error);
        }
    }

    stopServer(callback){
        this.Server.handler.say("Server will stop");
        this.Server.handler.stop(0, false, false, false).then(()=>{
            if(this.Server.discord){
                this.Server.discord.send(`The Minecraft server thats linked to this channel will be stopped`).then(()=>{
                    this.Server.die();
                    this.Server = null;
                });
            }
            else{
                this.Server = null;
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
        return require(`${this.Server.path}/ops.json`).map(e => e.name);
    }
    get daemon_status (){
        return daemon_status;
    }
    async report(){
        let server = null;
        if(this.Server){
            server = await this.getServerData();
            if(server != null){
                server.id = this.Server.id;
            }
        }
        return {
            'id': this.id,
            'host': this.host,
            'port': this.port,
            'srvRecord': this.srvRecord,
            'available_server': Slot.available_server,
            'status': this.status,
            'server':  server
        }
    }
    // SETTER
    set daemon_status(status){
        daemon_status = status;
        this.event.emit('daemonStatusChange', status);
    }
}

Slot.findIdByMotd = async function(motd){
    motd = motd.trim();
    try{
        let findServer = await Promise.all(config.Servers.map(async (server)=>{
            let match = false;
            const server_props = await fs.readFile(`${server.path}/server.properties`, {'encoding': 'utf8'});
            let server_motd = server_props.match(/motd=(.)+/g)
            if(server_motd != null){
                server_motd = server_motd[0].split('=')[1].trim();
                match = server_motd == motd;
            }
            return {'id': server.id, 'match': match};
        }));
        const found = findServer.find(e => e.match);
        return found ? found.id : null;
    }
    catch(error){
        console.error(error);
        return null;
    }
}

Slot.available_server = config.Servers.map(e => ({'id': e.id}));

// PRIVATE VAR
let daemon_status = "";
let suspend_server_start = false;

module.exports = Slot;