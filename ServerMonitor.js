const fs = require('fs');
const {get} = require('https');
const {exec, execSync, spawn} = require('child_process');
const Event = require('events');
const net = require('net');
const CronJob = require('cron').CronJob;
const Handler = require('./handler.js');
const Discord = require('./discord.js');
let config = require('./config.json');

// LOAD RESOURCES
const death_messages = require('./resources/death_messages.json');
const swear_words = require('./resources/swear-words.json');
const swear_words_regex = swear_words.join('|');

module.exports = class {
    constructor(server_id = config.Servers[0]['id']){
        setServerId.bind(this)(server_id);
        this.monitor = {};
        this.handler = new Handler();
        this.temp_policy = require('./temp_policy.js');
        this.recent_system_temp = 0.00;
        this.recent_system_freq = 0;
        this.event = new Event();
        this.update_permission_requested = false;
        this.status = "not running";
        this.daemon_status = "not running";
        this.discord = new Discord(this.server_id);
        this.web_interface = false;
        
        this.checkDaemon();
    }
    reloadConfig(callback){
        this.event.emit('ConfigReload');
        fs.readFile('./config.json', (err, data)=> {
            if(err == null){
                try{
                    config = JSON.parse(data);
                    this.server_id = this.server_id;
                    this.restartDiscordService();
                    this.event.emit('ConfigReloaded');
                    callback(config);
                }
                catch(error){
                    console.error("Error while parsing reload config file", error);
                    callback(null);
                }
            }
            else{
                console.error("Error on reload Config", err);
                callback(null);
            }
        });
    }
    restartDiscordService(){
        if(this.discord){
            this.discord.logout();
            this.discord = new Discord(this.server_id);
            let command_init = {
                'temp': this.discordObeyCommandTemp,
                'list': this.discordObeyCommandList,
                'version': this.discordObeyCommandVersion,
                'banlist': this.discordObeyCommandBanlist,
            };
            this.discord.commands_obeyed.forEach(command_name => {
                if(command_init.hasOwnProperty(command_name)){
                    command_init[command_name].bind(this)();
                }
            })
        }
        else{
            console.error("Discord server is not running");
        }
    }
    restartCron(cron = config.ServerRestartInterval){
        this.restart_cron = new CronJob(cron, ()=>{
        //this.restart_cron = new CronJob(, ()=>{
            if(this.status != "running")
                return false;
            this.status = "restarting";
            this.handler.tellraw("@a", "Server will restart in 30 seconds", "yellow");
            this.handler.stop(30000, 5000, false, false).then(()=>{
                this.once('daemonStatusChange', (event)=>{
                    if(this.daemon_status == 'not running'){
                        console.log("restart closed", "starting...");
                        this.startServer(()=>{
                            this.status = "restartComplete";
                            this.event.emit('restartComplete');
                        });
                    }
                    else {
                        throw new Error("Daemon still Alive");
                    }
                });
            })
        }, null, true, 'Europe/Berlin');
        this.restart_cron.start();
    }
    updateCron(){
        this.update_cron = new CronJob(config.ServerCheckUpdateInterval, this.update, null, true, 'Europe/Berlin');
        this.update_cron.start();
    }
    checkDaemon(callback){
        exec("if screen -ls | grep -q 'minecraft'; then echo 1; else echo 0; fi",  (err, stdout, stderr)=>{
            console.log("Server Manager Status: " + this.status);
            if(stdout == 0){
                this.daemon_status = "not running";
                if(this.status == "running"){
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
    // MONITORING
    temperature(interval_time){
        this.monitor['temperatur'] = setInterval(()=> {
            this.checkDaemon();
            fs.readFile(config.CPUTemperature, (err, data)=>{
                if(err == null){
                    const temp = (data/1000).toFixed(2);
                    this.recent_system_temp = temp;
                    if(temp < this.handler.temp_threshold){
                        clearInterval(this.handler.shutdown_interval);
                        clearTimeout(this.handler.shutdown_timeout);
                        exec("screen -S minecraft -X stuff 'say Shutdown aborted.\n'");
                        this.handler.temp_threshold = false;
                        this.handler.stop_block = false;
                    }
                    for(let i = 0; i < this.temp_policy.length; i++){
                        if(temp <= this.temp_policy[i]['max_temp']){
                            let actions = this.temp_policy[i]["action"](temp);
                            for(let action in actions){
                                if(Array.isArray(actions[action]))
                                    this.handler[action](...actions[action]);
                                else
                                    this.handler[action](actions[action]);
                            }
                            break;
                        }
                    }
                }
                else{
                    console.error(err);
                    clearInterval(this.monitor['temperatur']);
                }
            });
        }, interval_time);
    }
    frequency(interval_time){
        this.monitor['frequency'] = setInterval(()=> {
            fs.readFile(config.CPUFrequency, {encoding: 'utf-8'}, (err, data)=>{
                if(err == null){
                    let freq = parseFloat(data.trim()) / 1000 / 1000;
                    this.recent_system_freq = freq;
                    this.handler.say(`CPU frequency is ${freq} Ghz`);
                }
                else{
                    console.error(err);
                    clearInterval(this.monitor['frequency']);
                }
            });
        }, interval_time);
    }
    webService(){
        this.web_interface = true;
        // OPEN SOCKET INTERFACE FOR A WEBSERVICE
        var server = net.createServer((socket)=> { //'connection' listener
            console.log('server connected');
            socket.on('data', async (data)=> {
                try{
                    data = JSON.parse(data.toString());
                }catch(error){
                    console.error("Request wasn't a valid json");
                }
                console.log(data);
                if(data.authentication){
                    switch(data.command.name){
                        case "getTemp":
                            socket.write(this.mcsw_msg_response(this.recent_system_temp.toString()))
                            socket.pipe(socket);
                            break;
                        case "stopServer":
                            this.stopServer((error, data)=>{
                                if(error == null){
                                    socket.write(this.mcsw_msg_response(data.message));
                                }
                                else{
                                    socket.write(this.mcsw_error_response(error, data.message));
                                }
                                socket.pipe(socket);
                            });
                            break;
                        case "startServer":
                            this.startServer(data.command.args.id, (error, data) => {
                                if(error == null){
                                    socket.write(this.mcsw_msg_response(data.message));
                                }
                                else{
                                    socket.write(this.mcsw_error_response(error, data.message));
                                }
                                socket.pipe(socket);
                            });
                            socket.write(this.mcsw_msg_response("Server start has been initialized", true));
                            socket.pipe(socket);
                            break;
                        default:
                            socket.write("Command not supported");
                            socket.pipe(socket);
                    }
                }
                else{
                    socket.write("Authentication failed");
                    socket.pipe(socket);
                }
            });
            socket.on('end', function() {
                console.log('server disconnected');
            });
        });
        server.listen(8124, function() { //'listening' listener
            console.log('Socket (8124) is listening');
        });
    }
    banFlying(){
        this.mc_server.on("newLogLine", (line)=>{
            // CHECK IF SOMEONE IS BEEN Kicked for flying
            let check = line.content.match(/(.)* lost connection: Flying is not enabled on this server/);
            if(check != null){
                let flying_player = check[0].split(" ")[0];
                this.handler.ban(flying_player, "Flying is not allowed", '24h');
                this.discord.send(`${flying_player} is banned for 24 hours because of flying`);
                this.handler.say(`${flying_player} is banned for 24 hours because of flying`);
            }
        });
    }
    checkKill(){
        this.mc_server.on("newLogLine", (line)=>{
            // CHECK IF SOMEONE IS DEAD
            let check = null;
            let killed_by = null;
            for(let i = 0; i < death_messages.length; i++){
                const check_kill = new RegExp(`([^<>]+) ${death_messages[i]}`);
                check = line.content.match(check_kill);
                if(check != null){
                    if(i < 28){
                        killed_by = line.content.split(" ").slice(-1)   
                    }
                    break;
                }
            }
            if(check != null){
                let dying_player = check[0].split(" ")[0];
                let death_message = "";
                if(killed_by != null){
                    death_message = config.Discord.killMessage.replace('{{killed_by}}', killed_by).replace('{{dying_player}}', dying_player);
                }
                else{
                    death_message = config.Discord.deathMessage.replace('{{dying_player}}', dying_player);
                }
                this.discord.send(death_message);
            }
        });
    }
    antiToxicity(){
        this.mc_server.on("newLogLine", (line)=>{
            if(line.message && line.initiator && line.initiator != "Server"){
                if(line.message.match(swear_words_regex)){
                    this.handler.say(`Watch your mouth ${line.initiator}, you filthy bastard.`);
                }
            }
        });
    }
    discordObeyCommandList(){
        this.discord.obeyCommand('list', async ()=>{
            let playerList = await this.mc_server.getPlayerList();
            return `Aktuell ${playerList.length == 1 ? 'ist' : 'sind'} ${playerList.length} Spieler online. ${playerList.length > 0 ? `Online ${playerList.length == 1 ? 'ist' : 'sind'}:\r\n${playerList.join('\r\n')}` : ""}`;
        });
    }
    discordObeyCommandVersion(){
        this.discord.obeyCommand('version', async ()=> await this.mc_server.getCurrentVersion());
    }
    discordObeyCommandTemp(){
        this.discord.obeyCommand('temp', () => `Server temperature is currently at: ${this.recent_system_temp}°C`);
    }
    async discordObeyCommandBanlist(){
        const getBanlist = async ()=>{
            const banned_players = await this.mc_server.getBanlist();
            if(banned_players.length == 0){
                return `Currently are ${banned_players.length} player banned.`;
            }
            else{
                const banned_players_str = banned_players.map(e => '> ' + e.name).join('\r\n');
                if(banned_players.length == 1){
                    return `Currently is ${banned_players.length} player banned. Banned is:\r\n${banned_players_str}`;
                }
                else{
                    return `Currently are ${banned_players.length} player banned. Banned are:\r\n${banned_players_str}`;
                }
            }
        };
        this.discord.obeyCommand('banlist', getBanlist);
    }
    discordUpdatePresence(){
        const status = {
            'name': ``,
            'type': ``
        }
        this.discord.updatePresence();
    }
    async update(){
        if(this.status != "running")
            return false;
        if(this.update_permission_requested){
            console.log("Permission to update already requested")
            return false;
        }
        if(await this.newVersionAvailable() == false){
            console.log("Already running on the latest version")
            return false;
        }
        // ASK FOR PERMITION TO UPDATE PAPER ( ONLY OP's CAN CONFIRM )
        this.ops.forEach(player => {
            this.handler.tellraw(player, "A new version of paper is available, do you want to install the new Version? (Yes, No)", "yellow");
        });
        console.log(this.mc_server.ServerUpdateHost + this.mc_server.ServerUpdatePath, `.${config.DownloadDir}/paper-${Date.now()}.jar`);
        let confirm_update = (line)=>{
            if(this.ops.findIndex(e=> e == line.initiator) != -1){
                if(this.status != "running")
                    return;
                if(line.message == 'Yes'){
                    this.status = "updating";
                    this.mc_server.event.removeListener('newLogLine', confirm_update);
                    this.handler.tellraw(line.initiator, "Downloading new update...", "green");
                    this.handler.tellraw("@a", "Server will be updated. The server will shutdown in 30 seconds and will come back as soon as the update is completed.", "red");
                    this.handler.stop(30000, 5000, false, false).then(()=> {
                        // DOWNLOAD SERVER FILES
                        this.download(this.mc_server.ServerUpdateHost + this.mc_server.ServerUpdatePath, "." + config.DownloadDir + "/paper-" + Date.now() + ".jar", (output_file)=>{
                            this.status = "update installing";
                            this.emit("updateInstalling");
                            // INSTALL PROCESS
                            console.log("Installing new version of minecraft...");
                            //console.log(execSync(`ls -l .${config.DownloadDir}`).toString().trim());
                            fs.unlinkSync(this.mc_server.path + this.mc_server.ServerExecutable);
                            fs.renameSync(output_file, this.mc_server.path + this.mc_server.ServerExecutable);
                            this.status = "update installed";
                            this.handler.emit("updateInstalled");
                            this.startServer();
                        });
                    });
                }
                else{
                    this.handler.tellraw(line.initiator, "Not updating", "black");
                }
            }
        };
        this.mc_server.on('newLogLine', confirm_update);
        this.update_permission_requested = true;
    }
    download(url, output_file, callback){
        const download = fs.createWriteStream(output_file);
        console.log("downloading");
        const request = get("https://" + url, function(response) {
            response.pipe(download);
            response.on('error', (e) => console.error(e));
        });
        request.on('finish', ()=> callback(output_file));
        request.on('error', (e) => console.error(e));
    }

    mcsw_data_response(data, message, keep_alive = false){
        return JSON.stringify({'error': null, 'data':{...data, 'message': message}, 'keep_alive': keep_alive});
    }

    mcsw_error_response(error, message, keep_alive = false){
        return JSON.stringify({'error': error, 'data':{'message': message}, 'keep_alive': keep_alive});
    }

    mcsw_msg_response(message, keep_alive = false){
        return JSON.stringify({'error': null, 'data':{'message': message}, 'keep_alive': keep_alive});
    }

    on(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.on(name, callback);
    }
    once(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.once(name, callback);
    }

    startServer(id, callback){
        if(!isNaN(id)){
            if(this.suspend_mc_start){
                callback("Starting and restarting are temporarily suspended", {})
                return;
            }
            if(this.status == "restarting" || this.status == "starting"){
                callback("An start up or restart is already in process.", {})
                return;
            }
            if(this.mc_server.id != id){
                //await this.discord.send(`The Minecraft server thats linked to this channel will be stopped`);
            }
            setServerId.bind(this)(id);
            if(this.daemon_status == "running"){
                this.handler.say("Server will stop on Web Request");
                this.status = "restarting";
                this.suspendMcStart();
                this.handler.stop(0, false, false, false).then(()=>{
                    this.once('restartClosed', ()=>{
                        try{
                            spawn('bash', [this.mc_server.bin, ...this.mc_server.binOptions], {
                                slient: true,
                                detached: true,
                                stdio: [null, null, null, 'ipc']
                            }).unref();
                            this.discord.on('ready', ()=>{
                                this.discord.send(`The Minecraft server thats linked to this channel has startet. You might be able to join the server in a minute.`);
                            });
                            callback(null, {'message': "Server has been started."});
                        }
                        catch(error){
                            callback("Current server have been stopped but something went wrong starting the desired server", {'message': error})
                            console.log(error);
                        }
                    });
                });
            }
            else{
                if(this.status == "not running"){
                    this.status = "starting";
                    this.suspendMcStart();
                    try{
                        spawn('bash', [this.mc_server.bin, ...this.mc_server.binOptions], {
                            slient: true,
                            detached: true,
                            stdio: [null, null, null, 'ipc']
                        }).unref();
                        this.discord.on('ready', ()=>{
                            this.discord.send(`The Minecraft server thats linked to this channel has startet. You might be able to join the server in a minute.`);
                        });
                        callback(null, {'message': "Server has been started."});
                    }
                    catch(error){
                        callback("An error occured starting the server.", {'message': error})
                        console.log(error);
                    }
                }
                else{
                    callback("Another process is currently running please check again later", {})
                }
            }
        }
    }
    stopServer(callback){
        this.handler.say("Server will stop on Web Request");
        this.discord.send(`The Minecraft server thats linked to this channel will be stopped`).then(()=>{
            this.handler.stop(0, false, false, false).then(()=>{
                callback(null, {'message': "Server has been stopped"});
            });
        })
    }

    suspendMcStart(){
        if(suspend_mc_start == false){
            suspend_mc_start = true;
            setTimeout(() => {
                suspend_mc_start = false;
            }, 3 * 60 * 1000);
        }
    }

    // GETTER
    get server_id(){
        return this.mc_server.id;
    }
    get node_version() {
        return execSync("node --version").toString().trim();
    }
    get ops() {
        return require(`${this.mc_server.path}/ops.json`).map(e => e.name);
    }
    get daemon_status (){
        return daemon_status;
    }
    get suspend_mc_start(){
        return suspend_mc_start;
    }
    // SETTER
    set daemon_status(status){
        daemon_status = status;
        this.event.emit('daemonStatusChange', status);
    }
};


// PRIVATE VAR
let daemon_status = "";
let suspend_mc_start = false;

// PRIVATE FUNCTIONS

function setServerId(id){
    const mc_server_config = config.Servers.find(server => server.id == id);
    if(mc_server_config){
        const mc_server = require(`./Server/${mc_server_config.type}`);
        this.mc_server = new mc_server(mc_server_config);
        if(this.discord){
            this.restartDiscordService();
        }
        //fs.writeFileSync('./DaemonEnv', "SERVER_ID=" + id);
    }
}