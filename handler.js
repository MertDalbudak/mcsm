const {exec, execSync} = require('child_process');
const fs = require('fs').promises;
const moment = require('moment');
const config = require('./config.json');

module.exports = class {
    constructor(){
        this.shutdown_timeout = 0;
        this.shutdown_interval = 0;
        this.restart_timeout = 0;
        this.restart_interval = 0;
        this.temp_threshold = false;
        this.stop_block = false;
    }

    // SHUTDOWN SERVER AND EXITS
    kill(kill_all = true, callback){
        exec("screen -S minecraft -X stuff 'stop\n'", (err, stdout, stderr)=> {
            if(kill_all)
                process.exit();
            if(callback)
                callback();
        });
    }

    // SHUTDOWN WITH
    stop(timeout, announce_server = false, temp_threshold = true, kill = true){
        if(isNaN(timeout)){
            console.error("First parameter must be number, " + typeof timeout + " given.");
            return false;
        }
        if(this.stop_block){
            console.warn("Ongoing server stop in process cannot stop until it's aborted");
            return false;   
        }
        this.temp_threshold = temp_threshold;
        if(announce_server !== false) { // tell everonye that server is now shutting down
            let time_indication = timeout < 60000 ? Math.ceil(timeout / 1000) + " seconds" : (timeout < 3600000 ? Math.ceil(timeout / 60000) + " minutes" : Math.ceil(timeout / 3600000) + " hours");
            this.shutdown_interval = setInterval(()=> {
                timeout -= announce_server;
                time_indication = timeout < 60000 ? Math.ceil(timeout / 1000) + " seconds" : (timeout < 3600000 ? Math.ceil(timeout / 60000) + " minutes" : Math.ceil(timeout / 3600000) + " hours");
                if(timeout > 0)
                    this.say("Shutting down in " + time_indication);
                else {
                    clearInterval(this.shutdown_interval);
                }
            }, announce_server);
            this.say("Shutting down in " + time_indication);
        }
        this.log("Stopping server in " + (timeout / 1000) + " seconds");
        this.stop_block = true;
        return new Promise((resolve)=>{
            this.shutdown_timeout = setTimeout(()=>{
                this.kill(kill, ()=>{
                    this.stop_block = false;
                    resolve(true);
                });
            }, timeout);
        });
    }

    // LOG
    log(...msg){
        console.log(...msg);
    }

    // SERVER ECHO
    say(msg, callback = null){
        if(msg == null)
            return false;
        msg = msg.replace("\n", '\\n'); // Escape \n
        exec("screen -S minecraft -X stuff 'say " + msg + "\n'", function(...args){
            if(callback != null)
                callback();
        });
    }
    saySync(...msg){
        if(msg == null)
            return false;
        if(Array.isArray(msg)){
            for(let i = 0; i < msg.length; i++){
                msg[i] = msg[i].replace("\n", '\\n'); // Escape \n
                execSync("screen -S minecraft -X stuff 'say " + msg[i] + "\n'");
            }
        }
        else{
            msg = msg.replace("\n", '\\n'); // Escape \n
            execSync("screen -S minecraft -X stuff 'say " + msg + "\n'");
        }
        
    }

    tellraw(player, msg, color, callback = null){
        if(msg == null)
            return false;
        msg = msg.replace("\n", '\\n'); // Escape \n
        exec("screen -S minecraft -X stuff 'tellraw " + player + " [{\"text\":\"" + msg + "\",\"color\":\"" + color + "\"}]\n'", function(...args){
            if(callback != null)
                callback();
        });
    }

    // MESSAGE PLAYER
    msg(player_name, msg){
        msg = msg.replace("\n", '\\n'); // Escape \n
        exec("screen -S minecraft -X stuff 'msg " + player_name + " " + msg + "\n'");
    }

    // BAN PLAYER
    ban(player_name, reason = "Banned by server monitor", ban_time = "forever"){
        execSync("screen -S minecraft -X stuff 'ban " + player_name + " \"" + reason + "\” \n'");
        if(ban_time != "forever"){
            let factor = 1;
            switch(ban_time.substr(ban_time.length -1)){
                case "m": // MINUTES
                    factor = 60 * 1000;
                    break;
                case "h": // HOURS
                    factor = 60 * 60 * 1000;
                    break;
                case "d": // DAYS
                    factor = 24 * 60 * 60 * 1000;
                    break;
                case "w": // WEEKS
                    factor = 7 * 24 * 60 * 60 * 1000;
                    break;
                default:
                    return;
            }
            let expire_date = Date.now() + (ban_time.substr(0, ban_time.length -1) * factor);
            setTimeout(async ()=> {
                let banned_players = await fs.readFile(config.ServerPath + '/banned-players.json',{'encoding': "utf-8"});
                banned_players = JSON.parse(banned_players);
                console.log(banned_players);
                if(Array.isArray(banned_players)){
                    let banned_player = banned_players.find((player) => player.name == player_name);
                    if(banned_player){
                        banned_player.expires = moment(expire_date).format('YYYY-MM-DD HH:mm:ss ZZ');
                        await fs.writeFile(config.ServerPath + '/banned-players.json', JSON.stringify(banned_players), {'encoding': "utf-8"});
                    }
                }
            }, 2000);
        }
    }
    addWhitelist(player_name){
        execSync("screen -S minecraft -X stuff 'whitelist add " + player_name + "\n'");
        execSync("screen -S minecraft -X stuff 'whitelist reload \n'");
    }
    removeWhitelist(player_name){
        execSync("screen -S minecraft -X stuff 'whitelist remove " + player_name + "\n'");
        execSync("screen -S minecraft -X stuff 'whitelist reload \n'");
    }
}