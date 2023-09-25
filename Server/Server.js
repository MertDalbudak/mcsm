const fs = require('fs').promises;
fs.watch = require('fs').watch; // USE CALLBACK WATCH INSTEAD OF fs/promise.watch

const Event = require('events');
const {get} = require('https');
const {exec, execSync} = require('child_process');
const CronJob = require('cron').CronJob;
const config = require('../config.json').Server;

const Handler = require('../Handler.js');
const Discord = require('../Discord.js');

const ops_path = "/ops.json";
const whitelist_path = "/whitelist.json";
const banned_ips_path = "/banned-ips.json";
const banned_players_path = "/banned-players.json";


// LOAD RESOURCES
const death_messages = require('../resources/death_messages.json');
const blacklist_words = require('../resources/blacklist-words.json');
const blacklist_words_regex = blacklist_words.join('|');


class Server {
    /**
     * 
     * @param {Number} id 
     */
    constructor(id){
        this.id = id;

        this.event = new Event();

        this.ServerExecutable;
        this.ServerUpdateHost;
        this.ServerUpdatePath;

        this.update_permission_requested = false;

        Server.getAvailableServers().then(conf => {
            this.config = conf.find(e => e.id == this.id);

            if(this.config == undefined){
                throw new Error(`No server with given id ${id} found.`);
            }
            
            this.handler = new Handler(this.config);
            this.discord = new Discord(this.config.discord);


            this.properties = this.config.properties;

            this.path = this.config.path;
            this.logPath = this.path + this.config.logPath;
            this.ops_path = this.path + ops_path;
            this.whitelist_path = this.path + whitelist_path;
            this.banned_ips_path = this.path + banned_ips_path;
            this.banned_players_path = this.path + banned_players_path;

            this.discord.on('ready', ()=>{
                let discord_commands = {
                    'server': this.discordObeyCommandServer,
                    'temp': this.discordObeyCommandTemp,
                    'list': this.discordObeyCommandList,
                    'version': this.discordObeyCommandVersion,
                    'banList': this.discordObeyCommandBanlist,
                };
                let obeyCommand = this.config.discord.obeyCommand;
                for(let i = 0; i < obeyCommand.length; i++){
                    discord_commands[obeyCommand[i]].bind(this)();
                }
                let eventListener = this.config.discord.eventListener;
                for(let i = 0; i < eventListener.length; i++){
                    this[eventListener[i]].bind(this)();
                }
            });

            this.init();
        });
    }
    async init(){
        this.ops = fs.readFile(this.path + ops_path, {'encoding': 'utf-8'});
        this.whitelist = fs.readFile(this.whitelist_path, {'encoding': 'utf-8'});
        this.banned_ips = fs.readFile(this.banned_ips_path, {'encoding': 'utf-8'});
        this.banned_players = fs.readFile(this.banned_players_path, {'encoding': 'utf-8'});

        // Watch change of logs
        fs.watch(this.logPath, { 'persistent': true,  'interval': this.ServerLogCheckInterval}, (eventType, filename) => {
            this.event.emit("logChange", filename);
        });

        this.on('logChange', ()=>{
            this.logLastLines(1, (line)=>{
                console.log(line);
                let parsed_line = this.parseLine(line);
                if(parsed_line != null){
                    this.event.emit("newLogLine", parsed_line);
                }
            });
        });

        Promise.all([this.ops, this.whitelist, this.banned_ips, this.banned_players]).then(async()=>{
            this.ops = await this.ops;
            this.whitelist = await this.whitelist;
            this.banned_ips = await this.banned_ips;
            this.banned_players = await this.banned_players;

            // Watch change of ops
            fs.watch(this.ops_path, { 'persistent': true,  'interval': this.ServerLogCheckInterval}, (eventType, filename) => {
                this.event.emit("opsChange", filename);
                fs.readFile(this.ops_path, {'encoding': 'utf-8'}).then((data)=>{
                    this.event.emit("opsChange", {'current': this.ops, 'new': data});
                    this.ops = data;
                    this.event.emit("opsChanged", filename);
                });
            });

            // Watch change of whitelist
            fs.watch(this.whitelist_path, { 'persistent': true,  'interval': this.ServerLogCheckInterval}, (eventType, filename) => {
                fs.readFile(this.whitelist_path, {'encoding': 'utf-8'}).then((data)=>{
                    this.event.emit("whitelistChange", {'current': this.banned_players, 'new': data});
                    this.whitelist = data;
                    this.event.emit("whitelistChanged", filename);
                });
            });

            // Watch change of banned players
            fs.watch(this.banned_players_path, { 'persistent': true,  'interval': this.ServerLogCheckInterval}, (eventType, filename) => {
                fs.readFile(this.banned_players_path, {'encoding': 'utf-8'}).then((data)=>{
                    this.event.emit("bannedPlayersChange", {'current': this.banned_players, 'new': data});
                    this.banned_players = data;
                    this.event.emit("bannedPlayersChanged", filename);
                })
            });

            this.event.emit('ready');
        })
    }
    fileReadLastLines(path, n, callback){
        exec(`tail -n ${n} ${path}`, function(err, stdout, stderr){
            if(err || stderr){
                console.error(err);
                console.error(stderr);

                callback("ERROR READING LOG FILE")
            }
            else{
                callback(stdout.toString().trim());
            }
        });
    }
    fileReadLastLinesSync(path, n){
        return execSync(`tail -n ${n} ${path}`).toString().trim();
    }
    logLastLines(n, callback){
        this.fileReadLastLines(this.logPath, n, (lines)=>{
            callback(lines);
        });
    }
    logLastLinesSync(n){
        return this.fileReadLastLinesSync(this.logPath, n);
    }
    parseLine(line){
        let report_info = line.match(/^\[[0-9]*:[0-9]*:[0-9]*\] \[(.)*\]:/);
        if(report_info == null)
            return null;
        let parsed_line = {};
        parsed_line.content = line.substr(report_info[0].length).trim();
        parsed_line.timestamp = Date.now();
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

    async getCurrentVersion(){
        const server_data = await this.slot.getLiveData();
        if(server_data != null){
            return server_data.version.name;
        }
        return "Not found";
    }

    async getPlayerList(){
        const server_data = await this.slot.getLiveData();
        if(server_data != null){
            return server_data.players.online > 0 ? server_data.players.sample.map(e => e.name) : [];
        }
        return [];
    }

    antiToxicity(){
        this.on("newLogLine", (line)=>{
            if(line.message && line.initiator && line.initiator != "Server"){
                if(line.message.toLowerCase().match(blacklist_words_regex)){
                    this.handler.say(`Watch your mouth ${line.initiator}, you filthy bastard.`);
                }
            }
        });
    }

    banFlying(){
        this.on("newLogLine", (line)=>{
            // CHECK IF SOMEONE IS BEEN Kicked for flying
            let check = line.content.match(/(.)* lost connection: Flying is not enabled on this server/);
            if(check != null){
                let flying_player = check[0].split(" ")[0];
                this.handler.ban(flying_player, "Flying is not allowed", '24h');
                this.handler.say(`${flying_player} is banned for 24 hours because of flying`);
                this.discord.send(`${flying_player} is banned for 24 hours because of flying`);
            }
        });
    }

    checkDeath(){
        this.on("newLogLine", (line)=>{
            // CHECK IF SOMEONE IS DEAD
            let check = null;
            let killed_by = null;
            for(let i = 0; i < death_messages.length; i++){
                const check_kill = new RegExp(`([^<>]+) ${death_messages[i]}`);
                console.log(check_kill);
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
                    death_message = this.config.discord.killMessage.replace('{{killed_by}}', killed_by).replace('{{dying_player}}', dying_player);
                }
                else{
                    death_message = this.config.discord.deathMessage.replace('{{dying_player}}', dying_player);
                }
                this.discord.send(death_message);
            }
        });
    }
    discordObeyCommandServer(){
        this.discord.obeyCommand('server', () => `You can connect to the server via: ${this.slot.domain}`);
    }
    discordObeyCommandList(){
        this.discord.obeyCommand('list', async ()=>{
            let playerList = await this.getPlayerList();
            return `Aktuell ${playerList.length == 1 ? 'ist' : 'sind'} ${playerList.length} Spieler online. ${playerList.length > 0 ? `Online ${playerList.length == 1 ? 'ist' : 'sind'}:\r\n${playerList.join('\r\n')}` : ""}`;
        });
    }
    discordObeyCommandVersion(){
        this.discord.obeyCommand('version', async ()=> await this.getCurrentVersion());
    }
    discordObeyCommandTemp(){
        this.discord.obeyCommand('temp', () => `Server temperature is currently at: ${this.recent_system_temp}°C`);
    }
    async discordObeyCommandBanlist(){
        const getBanlist = async ()=>{
            const banned_players = await this.getBanlist();
            if(banned_players.length == 0){
                return `No players are banned currently.`;
            }
            else{
                const banned_players_str = banned_players.map(e => '> ' + e.name).join('\r\n');
                if(banned_players.length == 1){
                    return `Currently ${banned_players.length} is player banned. Banned is:\r\n${banned_players_str}`;
                }
                else{
                    return `Currently ${banned_players.length} are players banned. Banned are:\r\n${banned_players_str}`;
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
        console.log(this.ServerUpdateHost + this.ServerUpdatePath, `.${this.slot.DownloadPath}/paper-${Date.now()}.jar`);
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
                        this.download(this.ServerUpdateHost + this.ServerUpdatePath, "." + this.slot.DownloadPath + "/paper-" + Date.now() + ".jar", (output_file)=>{
                            this.status = "update installing";
                            this.emit("updateInstalling");
                            // INSTALL PROCESS
                            console.log("Installing new version of minecraft...");
                            //console.log(execSync(`ls -l .${this.slot.DownloadPath}`).toString().trim());
                            fs.unlinkSync(this.path + this.ServerExecutable);
                            fs.renameSync(output_file, this.path + this.ServerExecutable);
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
        this.on('newLogLine', confirm_update);
        this.update_permission_requested = true;
    }

    download(url, output_file, callback){
        const download_file = fs.createWriteStream(output_file);
        console.log("downloading");
        get(url, function(response) {
            response.pipe(download_file);
            download_file.on('finish', function() {
                console.log("DOWNLOADING COMPLETE");
                download_file.close(() => callback());  // close() is async, call cb after close completes.
            });
        }).on('error', function(err) { // Handle errors
            fs.unlink(DOWNLOAD_PAPER_JAR); // Delete the file async. (But we don't check the result)
            if (callback)
                callback(err.message);
        });
    }

    backupServer(callback){
        // CREATE BACKUP
        let backup_path, counter = 0;
        do{
            let filename_suffix = counter > 0 ? `(${counter})` : "";
            backup_path = `backups/paper-backup${filename_suffix}.zip`;
            counter++;
        }while(fs.existsSync(backup_path));
    
        exec(`zip -9 -r '${backup_path}' server`, (error, stdout, stderr) => {
            if(error != null && stderr != null){
                throw new Error(error);
            }
            callback();
        });
    }


    /**
     * 
     * @param {Number} port 
     * @returns {boolean}
     */
    async setPort(port){
        try{
            this.properties = this.properties.replace(/query\.port=[0-9]+/, `query.port=${port}`);
            this.properties = this.properties.replace(/server-port=[0-9]+/, `server-port=${port}`);
            await fs.writeFile(`${this.path}/server.properties`, this.properties, {'encoding': 'utf8'});
        }
        catch(error){
            console.error(error);
            return false;
        }
        finally{
            return true;
        }
    }

    on(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.on(name, callback);
    }
    once(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.once(name, callback);
    }

    die(){
        if(this.discord){
            this.discord.logout();
            this.discord = null;
        }
    }

    // GETTER
    get currentPlayerCount(){
        return this.currentPlayerCount.length();
    }
}

Server.getAvailableServers = async () =>{
    let server_list = [];
    for(let i = 0; i < config.path.length; i++){
        let path = config.path[i];
        let dir_list = null;
        try{
            dir_list = await fs.readdir(path, { withFileTypes: true });
        }catch(error){
            console.error(error);
            continue;
        }
        for(let j = 0; j < dir_list.length; j++){
            if(!dir_list[j].isDirectory()){
                continue;
            }
            let server_dirname = dir_list[j].name;
            let server_path = `${path}/${server_dirname}`;
            let server_data = null;
            try{
                server_data = JSON.parse(await fs.readFile(`${server_path}/plugins/mcsm/config.json`, {encoding: 'utf-8'}));
                server_data.properties = await fs.readFile(`${server_path}/server.properties`, {encoding: 'utf-8'});
            }
            catch(error){
                // console.error(error);
                continue;
            }
            server_data.path = server_path;
            server_list.push(server_data);
        }
    }
    return server_list;
}

Server.findIdByMotd = async function(motd){
    motd = motd.trim();
    const servers = await Server.available_servers;
    try{
        for(let i = 0; i < servers.length; i++){
            let server = servers[i];

            let server_motd = server.properties.match(/motd=(.)+/g);
            if(server_motd != null){
                server_motd = server_motd[0].split('=')[1].trim();
                if(server_motd == motd){
                    return server.id;
                }
            }
        }
    }
    catch(error){
        console.error(error);
        return null;
    }
}

Server.available_servers = Server.getAvailableServers();

module.exports = Server;