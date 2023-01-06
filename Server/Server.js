const fs = require('fs');
const Event = require('events');
const {exec, execSync} = require('child_process');
const ops_path = "/ops.json";
const whitelist_path = "/whitelist.json";
const banned_ips_path = "/banned-ips.json";
const banned_players_path = "/banned-players.json";


class Server {
    constructor(config){
        Object.assign(this, config);
        this.currentPlayers = [];
        this.maxPlayers = 0;
        this.ServerExecutable;
        this.ServerUpdateHost;
        this.ServerUpdatePath;

        this.bin_path = `/home/pi/src/mcsm/bin/${this.bin}`;

        this.ops_path = this.path + ops_path;
        this.whitelist_path = this.path + whitelist_path;
        this.banned_ips_path = this.path + banned_ips_path;
        this.banned_players_path = this.path + banned_players_path;

        this.ops = fs.readFileSync(this.path + ops_path, {'encoding': 'utf-8'});
        this.whitelist = fs.readFileSync(this.whitelist_path, {'encoding': 'utf-8'});
        this.banned_ips = fs.readFileSync(this.banned_ips_path, {'encoding': 'utf-8'});
        this.banned_players = fs.readFileSync(this.banned_players_path, {'encoding': 'utf-8'});

        this.event = new Event();

        // Watch change of logs
        fs.watchFile(this.path + this.logPath, { 'persistent': true,  'interval': this.ServerLogCheckInterval}, async (eventType, filename) => {
            this.event.emit("logChange", filename);
        });

        // Watch change of ops
        fs.watchFile(this.ops_path, { 'persistent': true,  'interval': this.ServerLogCheckInterval}, async (eventType, filename) => {
            this.event.emit("opsChange", filename);
            this.ops = fs.readFileSync(this.ops_path, {'encoding': 'utf-8'});
            this.event.emit("opsChanged", filename);
        });

        // Watch change of whitelist
        fs.watchFile(this.whitelist_path, { 'persistent': true,  'interval': this.ServerLogCheckInterval}, async (eventType, filename) => {
            let new_whitelist = fs.readFileSync(this.whitelist_path, {'encoding': 'utf-8'})
            this.event.emit("whitelistChange", {'current': this.banned_players, 'new': new_whitelist});
            this.whitelist = new_whitelist;
            this.event.emit("whitelistChanged", filename);
        });

        // Watch change of banned players
        fs.watchFile(this.banned_players_path, { 'persistent': true,  'interval': this.ServerLogCheckInterval}, async (eventType, filename) => {
            const new_banned_players = fs.readFileSync(this.banned_players_path, {'encoding': 'utf-8'});
            this.event.emit("bannedPlayersChange", {'current': this.banned_players, 'new': new_banned_players});
            this.banned_players = new_banned_players;
            this.event.emit("bannedPlayersChanged", filename);
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
        this.fileReadLastLines(this.path + this.logPath, n, (lines)=>{
            callback(lines);
        });
    }
    logLastLinesSync(n){
        return this.fileReadLastLinesSync(this.path + this.logPath, n);
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
    on(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.on(name, callback);
    }
    once(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.once(name, callback);
    }

    get currentPlayerCount(){
        return this.currentPlayerCount.length();
    }
}

module.exports = Server;