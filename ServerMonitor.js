const fs = require('fs');
const {get} = require('https');
const {exec, execSync} = require('child_process');
const Event = require('events');
const CronJob = require('cron').CronJob;
const Handler = require('./handler.js');
const Discord = require('./discord.js');
let config = require('./config.json');

// LOAD RESOURCES
const death_messages = require('./resources/death_messages.json');

module.exports = class {
    constructor(server_id = config.Servers[0]['id']){
        this.mc_server;
        this.server_id = server_id;
        this.monitor = {};
        this.handler = new Handler();
        this.temp_policy = require('./temp_policy.js');
        this.recent_system_temp = 0.00;
        this.recent_system_freq = 0;
        this.event = new Event();
        this.update_permission_requested = false;
        this.status = "running";
        this.DaemonStatus = "not running";
        this.Discord = new Discord(this.server_id);

        // Watch change of logs
        fs.watchFile(this.mc_server.path + this.mc_server.logPath, { 'persistent': true,  'interval': config.ServerLogCheckInterval}, (eventType, filename) => {
            this.event.emit("logChange", filename);
        });

        // Watch change of ops
        fs.watchFile(this.mc_server.path + '/ops.json', { 'persistent': true,  'interval': config.ServerLogCheckInterval}, (eventType, filename) => {
            this.event.emit("opsChange", filename);
        });

        this.on('logChange', ()=>{
            this.logLastLines(1, (line)=>{
                console.log(line);
                let parsed_line = this.parseLine(line);
                if(parsed_line != null)
                    this.event.emit("newLogLine", parsed_line);
            });
        });
        this.checkDaemon();
    }
    parseLine(line){
        let report_info = line.match(/^\[[0-9]*:[0-9]*:[0-9]*\] \[(.)*\]:/);
        if(report_info == null)
            return null;
        let parsed_line = {};
        parsed_line.content = line.substr(report_info[0].length).trim();
        parsed_line.date = new Date(Date.UTC());
        try{
            console.log(parsed_line.content.match(/\<.*\>|\[Server\]/));
            parsed_line.initiator = parsed_line.content.match(/\<.*\>|\[Server\]/)[0].slice(1,-1); // STRING (NAME OF PLAYER)
        } catch(err){
            parsed_line.initiator = null;
        }
        parsed_line.message = parsed_line.initiator != null ? parsed_line.content.substr(parsed_line.initiator.length + 2).trim() : null;


        return parsed_line;
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
        this.Discord.logout();
        this.Discord = new Discord(this.server_id);
        let command_init = {
            'temp': this.discord_obey_command_temp,
            'list': this.discord_obey_command_list,
            'version': this.discord_obey_command_version,
            'banlist': this.discord_obey_command_banlist,
        };
        this.Discord.commands_obeyed.forEach(command_name => {
            if(command_init.hasOwnProperty(command_name)){
                command_init[command_name]();
            }
        })
    }
    restartCron(cron = config.ServerRestartInterval){
        this.restart_cron = new CronJob(cron, ()=>{
        //this.restart_cron = new CronJob(, ()=>{
            if(this.status != "running")
                return false;
            this.status = "restarting";
            this.handler.tellraw("@a", "Server will restart in 30 seconds", "yellow");
            this.handler.stop(30000, 5000, false, false).then(()=>{
                this.once('DaemonStatusChange', (event)=>{
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
            if(stdout == 0){
                this.daemon_status = "not running";
                if(this.status == "running"){
                    console.error("Minecraft server is not running anymore", "Exiting...");
                    process.exit();
                }
                else{
                    switch(this.status){
                        case "restarting":
                            this.event.emit("restartClosed");
                            this.status = "restarted";
                            break;
                        case "updating":
                            this.event.emit("updateClosed");
                            this.status = "updateDownloading";
                            break;
                    }
                }
            }
            else{
                this.daemon_status = "running";
                if(this.status == "updateComplete" || this.status == "restartComplete"){
                    this.status = "running";
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
    banFlying(){
        this.on("newLogLine", (line)=>{
            // CHECK IF SOMEONE IS BEEN Kicked for flying
            let check = line.content.match(/(.)* lost connection: Flying is not enabled on this server/);
            if(check != null){
                let flying_player = check[0].split(" ")[0];
                this.Discord.send(`${flying_player} is banned for 24 hours because of flying`)
                this.handler.say(`${flying_player} is banned for 24 hours because of flying`);
                this.handler.ban(flying_player, "Flying is not allowed", '24h');
            }
        });
    }
    checkKill(){
        this.on("newLogLine", (line)=>{
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
                this.Discord.send(death_message);
            }
        });
    }
    discord_obey_command_list(){
        this.Discord.obeyCommand('list', async ()=>{
            let playerList = await this.mc_server.utils.checkPlayerList();
            return `Aktuell ${playerList.length == 1 ? 'ist' : 'sind'} ${playerList.length} Spieler online. ${playerList.length > 0 ? `Online ${playerList.length == 1 ? 'ist' : 'sind'}:\r\n${playerList.join('\r\n')}` : ""}`;
        });
    }
    discord_obey_command_version(){
        this.Discord.obeyCommand('version', async ()=>{
            let version = await this.mc_server.utils.getCurrentVersion();
            console.log(version);
            return version;
        });
    }
    discord_obey_command_temp(){
        this.Discord.obeyCommand('temp', () => `Server temperature is currently at: ${this.recent_system_temp}°C`);
    }
    discord_obey_command_banlist(){
        const get_banlist = ()=> new Promise((resolve)=>{
            fs.readFile(this.mc_server.path + '/banned-players.json', (err, data)=> {
                if(err == null){
                    try{
                        const banned_players = JSON.parse(data);
                        resolve(`Currently ${banned_players.length == 1 ? 'is' : 'are'} ${banned_players.length} player banned.${banned_players.length > 0 ? ` Banned ${banned_players.length == 1 ? 'is' : 'are'}:\r\n${banned_players.map(e => '> ' + e.name).join('\r\n')}` : ''}`);
                    }
                    catch(err){
                        throw new Error('Failed to retrieve banned player list')
                    }
                }
            })
        });
        this.Discord.obeyCommand('banlist', get_banlist);
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
        console.log(this.mc_server.utils.ServerUpdateHost + this.mc_server.utils.ServerUpdatePath, `.${config.DownloadDir}/paper-${Date.now()}.jar`);
        let confirm_update = (line)=>{
            if(this.ops.findIndex(e=> e == line.initiator) != -1){
                if(this.status != "running")
                    return;
                if(line.message == 'Yes'){
                    this.status = "updating";
                    this.event.removeListener('newLogLine', confirm_update);
                    this.handler.tellraw(line.initiator, "Downloading new update...", "green");
                    this.handler.tellraw("@a", "Server will be updated. The server will shutdown in 30 seconds and will come back as soon as the update is completed.", "red");
                    this.handler.stop(30000, 5000, false, false).then(()=> {
                        // DOWNLOAD SERVER FILES
                        this.download(this.mc_server.utils.ServerUpdateHost + this.mc_server.utils.ServerUpdatePath, "." + config.DownloadDir + "/paper-" + Date.now() + ".jar", (output_file)=>{
                            this.status = "updateInstalling";
                            this.emit("updateInstalling");
                            // INSTALL PROCESS
                            console.log("Installing new version of minecraft...");
                            //console.log(execSync(`ls -l .${config.DownloadDir}`).toString().trim());
                            fs.unlinkSync(this.mc_server.path + this.mc_server.utils.ServerExecutable);
                            fs.renameSync(output_file, this.mc_server.path + this.mc_server.utils.ServerExecutable);
                            this.status = "updateComplete";
                            this.handler.emit("updateComplete");
                            this.startServer();
                        });
                    });
                }
                else{
                    this.handler.tellraw(line.initiator, "Not updating", "black");
                }
            }
        };
        this.on('newLogLine', confirm_update);
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

    fileReadLastLines(path, n, callback){
        exec("tail -n " + n + " " + path, function(err, stdout, stderr){
            callback(stdout.toString().trim());
        });
    }
    fileReadLastLinesSync(path, n){
        console.log(execSync(`tail -n ${n} ${path}`).toString().trim());
        return execSync(`tail -n ${n} ${path}`).toString().trim();
    }
    logLastLines(n, callback){
        this.fileReadLastLines(this.mc_server.path + this.mc_server.logPath, n, (lines)=>{
            callback(lines);
        });
    }
    logLastLinesSync(n){
        return this.fileReadLastLinesSync(this.mc_server.path + this.mc_server.logPath, n)
    }
    on(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.on(name, callback);
    }
    once(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.once(name, callback);
    }

    startServer(callback){
        this.checkDaemon((state)=>{
            if(!state){
                exec(this.mc_server.bin, (err, stdout, stderr) =>{
                    console.log(err);
                    console.log(stdout);
                    console.log(stderr);
                    setTimeout(()=> callback(), 3000);
                });
            }
            console.log(state);
        });
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
        return this.DaemonStatus;
    }
    // SETTER
    set daemon_status(status){
        this.DaemonStatus = status;
        this.event.emit('DaemonStatusChange', status);
    }
    set server_id(id){
        this.mc_server = config.Servers.find(server => server.id == id);
        const server_utils = require(`./Server/${this.mc_server.type}`);
        this.mc_server.utils = {
            'newVersionAvailable': server_utils.newVersionAvailable.bind(this),
            'getCurrentVersion': server_utils.getCurrentVersion.bind(this),
            'checkPlayerList': server_utils.checkPlayerList.bind(this),
            'ServerExecutable': server_utils.ServerExecutable,
            'ServerUpdateHost': server_utils.ServerUpdateHost,
            'ServerUpdatePath': server_utils.ServerUpdatePath
        }
    }
};
