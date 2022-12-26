const fs = require('fs');
const {get} = require('https');
const {exec, execSync} = require('child_process');
const Event = require('events');
const CronJob = require('cron').CronJob;
const config = require('./config.json');
const Server = require('./Server/' + config.ServerType);
const Handler = require('./handler.js');
const Discord = require('./discord.js');

// LOAD RESOURCES
const death_messages = require('./resources/death_messages.json');
const { timeout } = require('cron');

config.ServerExecutable = Server.ServerExecutable;
config.ServerUpdateHost = Server.ServerUpdateHost;
config.ServerUpdatePath = Server.ServerUpdatePath;

module.exports = class {
    constructor(){
        this.server_dir = config.ServerPath;
        this.monitor = {};
        this.handler = new Handler();
        this.temp_policy = require('./temp_policy.js');
        this.recent_system_temp = 0.00;
        this.recent_system_freq = 0;
        this.event = new Event();
        this.update_permission_requested = false;
        this.status = "running";
        this.Discord = Discord;
        this.DaemonStatus = "not running";

        this.newVersionAvailable = Server.newVersionAvailable.bind(this);
        this.getCurrentVersion = Server.getCurrentVersion.bind(this);
        this.checkPlayerList = Server.checkPlayerList.bind(this);

        fs.watchFile(this.server_dir + config.ServerLogPath, { 'persistent': true,  'interval': config.ServerLogCheckInterval}, (eventType, filename) => {
            this.event.emit("logChange", filename);
        });

        fs.watchFile(this.server_dir + '/ops.json', { 'persistent': true,  'interval': config.ServerLogCheckInterval}, (eventType, filename) => {
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
                Discord.send(`${flying_player} is banned for 2 hours because of flying`)
                this.handler.say(`${flying_player} is banned for 2 hours because of flying`);
                this.handler.ban(flying_player, "Flying is not allowed", '2h');
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
                if(killed_by != null){
                    Discord.send(`${killed_by} ${dying_player} amina koydu`);
                }
                else{
                    Discord.send(dying_player + ' geberdi, hahahaha!');
                }
            }
        });
    }
    discord_obey_command_list(){
        Discord.obeyCommand('list', async ()=>{
            let playerList = await this.checkPlayerList();
            return `Aktuell ${playerList.length == 1 ? 'ist' : 'sind'} ${playerList.length} Spieler online. ${playerList.length > 0 ? `Online ${playerList.length == 1 ? 'ist' : 'sind'}:\r\n${playerList.join('\r\n')}` : ""}`;
        });
    }
    discord_obey_command_version(){
        Discord.obeyCommand('version', async ()=>{
            let version = await this.getCurrentVersion();
            console.log(version);
            return version;
        });
    }
    discord_obey_command_temp(){
        Discord.obeyCommand('temp', () => `Server temperature is currently at: ${this.recent_system_temp}°C`);
    }
    discord_obey_command_banlist(){
        const get_banlist = ()=> new Promise((resolve)=>{
            fs.readFile(config.ServerPath + '/banned-players.json', (err, data)=> {
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
        Discord.obeyCommand('banlist', get_banlist);
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
        console.log(config.ServerUpdateHost + config.ServerUpdatePath, `.${config.DownloadDir}/paper-${Date.now()}.jar`);
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
                        this.download(config.ServerUpdateHost + config.ServerUpdatePath, "." + config.DownloadDir + "/paper-" + Date.now() + ".jar", (output_file)=>{
                            this.status = "updateInstalling";
                            this.emit("updateInstalling");
                            // INSTALL PROCESS
                            console.log("Installing new version of minecraft...");
                            //console.log(execSync(`ls -l .${config.DownloadDir}`).toString().trim());
                            fs.unlinkSync(config.ServerPath + config.ServerExecutable);
                            fs.renameSync(output_file, config.ServerPath + config.ServerExecutable);
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
        this.fileReadLastLines(this.server_dir + config.ServerLogPath, n, (lines)=>{
            callback(lines);
        });
    }
    logLastLinesSync(n){
        return this.fileReadLastLinesSync(this.server_dir + config.ServerLogPath, n)
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
                exec(config.bin, (err, stdout, stderr) =>{
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
    get node_version() {
        return execSync("node --version").toString().trim();
    }

    get ops() {
        return require(`${config.ServerPath}/ops.json`).map(e => e.name);
    }
    get daemon_status (){
        return this.DaemonStatus;
    }
    // SETTER
    set daemon_status(status){
        this.DaemonStatus = status;
        this.event.emit('DaemonStatusChange', status);
    }
};
