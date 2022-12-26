const { Client, GatewayIntentBits } = require('discord.js');
const config = require("./config.json");

const client = new Client({
    intents: [
        GatewayIntentBits.Guilds,
        GatewayIntentBits.GuildMessages,
        GatewayIntentBits.MessageContent
    ]
});

const channel_name = config.DISCORD_BOT_CHANNEL_NAME;
const channel_id = config.DISCORD_BOT_CHANNEL_ID;

let channel;

let supported_commands = [
    {
        'name': "list",
        'reply': (issuer)=>{

        },
        'active': false
    },
    {
        'name': "version",
        'reply': (issuer)=>{
            channel.send(`Checking server version...`);
        },
        'active': false
    },
    {
        'name': "temp",
        'reply': (issuer)=>{
            channel.send(`Checking server temperature...`);
        },
        'active': false
    },
    {
        'name': "banlist",
        'reply': (issuer)=>{
            channel.send(`Getting list of banned players...`);
        },
        'active': false
    }
];

client.on('ready', ()=> {
    channel = client.channels.cache.get(channel_id)
});

client.login(config.DISCORD_BOT_TOKEN);

module.exports = {
    'send': function(msg){
        channel.send(msg);
    },
    'obeyCommand': function(command_name, callback){
        const command = supported_commands.find(e => e.name === command_name);
        if(command){
            command.active = true;
            client.on('messageCreate', async (message) => {
                if (message.channel.id == channel_id) {
                    if(message.content == command_name){
                        message.react("👍");
                        command.reply(message.author.username);
                        try{
                            let reply_msg = await callback();
                            channel.send(reply_msg);
                        }
                        catch(err){
                            console.error("Couldn't reply to users request");
                        }
                    }
                }
            });
        }
        else{
            throw 'Obey command not supported';
        }
    },
    'on': client.on.bind(client)
};

client.on('messageCreate', async (message) => {
    if (message.channel.id == channel_id) {
        if(message.content == "help" || message.content == "hilfe"){
            message.react("👍");
            const help_text = `Folgende Befehle sind verfügbar:\n${supported_commands.filter(e => e.active).map(e => '> ' + e.name).join('\r\n')}`;
            channel.send(help_text);
        }
    }
});