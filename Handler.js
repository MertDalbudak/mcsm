const {exec, execSync} = require('child_process');
const fs = require('fs').promises;
const Event = require('events');
const moment = require('moment');
const config = require('./config.json');

module.exports = class {
    constructor(server_id){
        this.server_config = config.Servers.find(e => e.id == server_id);
        if(this.server_config == undefined){
            throw new Error(`Server handler couldn't find server in config file (Id: ${server_id})`);
        }
        this.event = new Event();
        this.screen_id = this.server_config.bin;
        this.shutdown_notify_interval = 0;
        this.restart_timeout = 0;
        this.restart_interval = 0;
        this.stop_block = false;
    }
    /**
     * 
     * @param {String} str 
     */
    inputSync(str){
        str = str.toString().trim().replace(/(\n|\r)/g, '\\\\');
        execSync(`screen -S ${this.screen_id} -X stuff '${str} \n'`);
    }
    input(str, callback){
        str = str.toString().trim().replace(/(\n|\r)/g, '\\\\');
        exec(`screen -S ${this.screen_id} -X stuff '${str} \n'`, callback);
    }
    // SHUTDOWN SERVER AND EXITS
    kill(kill_all = true, callback){
        this.input('stop', (err, stdout, stderr)=> {
            if(kill_all)
                process.exit();
            if(callback)
                callback();
        });
    }

    // SHUTDOWN WITH
    stop(timeout, announce_server = false, kill = false){
        if(isNaN(timeout)){
            console.error("First parameter must be number, " + typeof timeout + " given.");
            return false;
        }
        if(this.stop_block){
            console.warn("Ongoing server stop in process cannot stop until it's aborted");
            return false;   
        }
        if(announce_server !== false) { // tell everonye that server is now shutting down
            let time_indication = timeout < 60000 ? Math.ceil(timeout / 1000) + " seconds" : (timeout < 3600000 ? Math.ceil(timeout / 60000) + " minutes" : Math.ceil(timeout / 3600000) + " hours");
            this.shutdown_notify_interval = setInterval(()=> {
                timeout -= announce_server;
                time_indication = timeout < 60000 ? Math.ceil(timeout / 1000) + " seconds" : (timeout < 3600000 ? Math.ceil(timeout / 60000) + " minutes" : Math.ceil(timeout / 3600000) + " hours");
                if(timeout > 0)
                    this.say("Shutting down in " + time_indication);
                else {
                    clearInterval(this.shutdown_notify_interval);
                    
                }
            }, announce_server);
            this.say("Shutting down in " + time_indication);
        }
        this.log("Stopping server in " + (timeout / 1000) + " seconds");
        this.stop_block = true;
        return new Promise((resolve, reject)=>{
            const shutdown_timeout = setTimeout(()=>{
                interruptShutdown = () => false;
                this.kill(kill, ()=>{
                    this.stop_block = false;
                    this.event.emit('stopped');
                    resolve();
                });
            }, timeout);
            interruptShutdown = () =>{
                clearTimeout(shutdown_timeout);
                this.stop_block = false;
                reject();
                return true;
            }
        });
    }

    abortShutdown(){
        if(interruptShutdown()){
            this.say('Shutdown aborted');
        }
    }

    // LOG
    log(...msg){
        console.log(...msg);
    }

    // SERVER ECHO
    say(msg, callback = null){
        if(!msg){
            return false;
        }
        this.input(`say ${msg}`, callback);
    }
    saySync(...msg){
        if(msg.length < 1)
            return false;
        for(let i = 0; i < msg.length; i++){
            this.inputSync(`say ${msg[i]}`);
        }
    }

    tellraw(player, msg, color, callback = null){
        if(msg == null)
            return false;
        this.inputSync(`tellraw ${player} [{"text":"${msg}","color":"${color}"}]`, function(...args){
            if(callback != null)
                callback(...args);
        });
    }

    // MESSAGE PLAYER
    msg(player_name, msg){
        this.inputSync(`msg ${player_name} ${msg}`);
    }

    // BAN PLAYER
    ban(player_name, reason = "Banned by server monitor", ban_time = "forever"){
        this.inputSync(`ban ${player_name} "${reason}"`);
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
                let banned_players = await fs.readFile(this.server_config.path + '/banned-players.json',{'encoding': "utf-8"});
                banned_players = JSON.parse(banned_players);
                console.log(banned_players);
                if(Array.isArray(banned_players)){
                    let banned_player = banned_players.find((player) => player.name == player_name);
                    if(banned_player){
                        banned_player.expires = moment(expire_date).format('YYYY-MM-DD HH:mm:ss ZZ');
                        await fs.writeFile(this.server_config.path + '/banned-players.json', JSON.stringify(banned_players), {'encoding': "utf-8"});
                    }
                }
            }, 2000);
        }
    }
    /**
     * 
     * @param  {...String} player_name 
     */
    addWhitelist(...player_name){
        for(let i = 0; i < player_name.length; i++){
            this.inputSync(`whitelist add ${player_name[i].toString().trim()}`);
        }
        this.reloadWhitelist();
    }
    /**
     * 
     * @param  {...String} player_name 
     */
    removeWhitelist(...player_name){
        for(let i = 0; i < player_name.length; i++){
            this.inputSync(`whitelist remove ${player_name[i].toString().trim()}`);
        }
        this.reloadWhitelist();
    }
    reloadWhitelist(){
        this.inputSync(`whitelist reload`);
    }
}

let interruptShutdown = () => {
    console.error("There is no shutdown process to be canceled");
    return false;
};