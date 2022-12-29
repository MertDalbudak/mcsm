const { Client, GatewayIntentBits } = require('discord.js');
const config = require("./config.json");

let supported_commands = [
    {
        'name': "help",
        'reply': (issuer)=> `Following commands are available:\n${supported_commands.filter(e => e.active).map(e => '> ' + e.name).join('\r\n')}`,
        'active': false
    },
    {
        'name': "list",
        'reply': (issuer)=> null,
        'active': false
    },
    {
        'name': "version",
        'reply': (issuer)=> `Checking server version...`,
        'active': false
    },
    {
        'name': "temp",
        'reply': (issuer)=> `Checking server temperature...`,
        'active': false
    },
    {
        'name': "banlist",
        'reply': (issuer)=> `Getting list of banned players...`,
        'active': false
    }
];


module.exports = class{
    constructor(server_id){
        this.server_id = server_id;
        this.server_props = config.Servers.find(server => server.id == this.server_id).discord;
        this.commands_obeyed_counter = 0;
        this.client = new Client({
            intents: [
                GatewayIntentBits.Guilds,
                GatewayIntentBits.GuildMessages,
                GatewayIntentBits.MessageContent
            ]
        });
        this.client.on('ready', ()=> {
            this.channels = this.server_props.channels.map(ch => this.client.channels.cache.get(ch.id));
        });
        this.client.login(this.server_props.token);
    }
    send(msg, index = null){
        if(index){
            this.channels[index].send(msg);
        }
        else{
            this.channels.forEach(channel => {
                channel.send(msg);
            });
        }
    }
    obeyCommand(command_name, callback){
        const command = supported_commands.find(e => e.name === command_name);
        if(command){
            command.active = true;
            this.client.on('messageCreate', async (message) => {
                if (this.server_props.channels.find(ch => ch.id == message.channel.id)) {
                    if(message.content === config.Discord.commandPrefix + command_name){
                        message.react("👍");
                        let command_reply = command.reply(message.author.username);
                        if(command_reply != null){
                            message.channel.send(command_reply);
                        }
                        try{
                            if(callback){
                                let reply_msg = await callback();
                                console.log(reply_msg);
                                message.channel.send(reply_msg);
                            }
                        }
                        catch(err){
                            console.error("Couldn't reply to users request", err);
                        }
                    }
                }
            });
            this.commands_obeyed_counter++;
            if(this.commands_obeyed_counter == 1){
                this.obeyCommand('help');
            }
        }
        else{
            throw new Error('Obey command not supported');
        }
    }
    logout(callback){
        this.client.logout(()=> callback)
    }
    // GETTER
    get commands_obeyed(){
        return supported_commands.reduce((acc, curr) => {
            if(curr.active)
                acc.push(curr.name);
        }, []);
    }
};