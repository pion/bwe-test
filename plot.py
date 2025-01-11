# SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
# SPDX-License-Identifier: MIT

import argparse
import json

import datetime as dt
import matplotlib.dates as mdates
import matplotlib.pyplot as plt
import pandas as pd

from matplotlib.ticker import EngFormatter, PercentFormatter


def plotter(ax, data, params):
    defaults = {
            'linewidth': 0.5,
        }
    params = defaults | params
    out, = ax.plot(data, **params)
    return out


def stepper(ax, data, params):
    defaults = {
            'linewidth': 0.5,
        }
    params = defaults | params
    out, = ax.step(data.index, data.values, where='post', **params)
    return out


def scatter(ax, data, params):
    defaults = {
           's': 0.1,
           'linewidths': 0.5,
        }
    params = defaults | params
    out = ax.scatter(data.index, data.values, **params)
    return out


def read_rtcp(file, basetime):
    df = pd.read_csv(
            file,
            index_col=0,
            names=['time', 'rate'],
            header=None,
            usecols=[0, 1],
        )
    if not basetime:
        basetime = df.index[0]

    df.index = pd.to_datetime(df.index - basetime, unit='ms')
    df['rate'] = df['rate'].apply(lambda x: x * 8)
    df = df.resample('1s').sum()
    return df


def read_rtp(file, basetime):
    df = pd.read_csv(
            file,
            index_col=0,
            names=['time', 'rate'],
            header=None,
            usecols=[0, 6]
        )

    if not basetime:
        basetime = df.index[0]

    df.index = pd.to_datetime(df.index - basetime, unit='ms')
    df['rate'] = df['rate'].apply(lambda x: x * 8)
    df = df.resample('1s').sum()
    return df


def read_capacity(file, basetime):
    df = pd.read_csv(
            file,
            index_col=0,
            names=['time', 'bandwidth'],
            header=None,
            usecols=[0, 1],
        )
    if not basetime:
        basetime = df.index[0]

    df.index = pd.to_datetime(df.index - basetime, unit='ms')
    return df


def read_cc_qdelay(file, basetime):
    df = pd.read_csv(
            file,
            index_col=0,
            names=['time', 'queue delay'],
            header=None,
            usecols=[0, 2]
        )

    if not basetime:
        basetime = df.index[0]

    df.index = pd.to_datetime(df.index - basetime, unit='ms')
    return df


def read_cc_target_rate(file, basetime):
    df = pd.read_csv(
            file,
            index_col=0,
            names=['time', 'target bitrate'],
            header=None,
            usecols=[0, 1]
        )

    if not basetime:
        basetime = df.index[0]

    df.index = pd.to_datetime(df.index - basetime, unit='ms')
    return df


def read_rtp_loss(send_file, receive_file, basetime):
    df_send = pd.read_csv(
            send_file,
            index_col=1,
            names=['time_send', 'nr'],
            header=None,
            usecols=[0, 8],
        )
    df_receive = pd.read_csv(
            receive_file,
            index_col=1,
            names=['time_receive', 'nr'],
            header=None,
            usecols=[0, 8],
        )

    if not basetime:
        basetime = df_send.index[0]

    df_all = df_send.merge(df_receive, on=['nr'], how='left', indicator=True)
    df_all.index = pd.to_datetime(df_all['time_send'] - basetime, unit='ms')
    df_all['lost'] = df_all['_merge'] == 'left_only'
    df_all = df_all.resample('1s').agg({'time_send': 'count', 'lost': 'sum'})
    df_all['loss_rate'] = df_all['lost'] / df_all['time_send']

    df = df_all.drop('time_send', axis=1)
    df = df.drop('lost', axis=1)

    return df


def read_rtp_latency(send_file, receive_file, basetime):
    df_send = pd.read_csv(
            send_file,
            index_col=1,
            names=['time_send', 'nr'],
            header=None,
            usecols=[0, 8],
        )
    df_receive = pd.read_csv(
            receive_file,
            index_col=1,
            names=['time_receive', 'nr'],
            header=None,
            usecols=[0, 8],
        )

    if not basetime:
        basetime = df_send.index[0]

    df = df_send.merge(df_receive, on='nr')
    df['diff'] = (df['time_receive'] - df['time_send']) / 1000.0
    df['time'] = pd.to_datetime(df['time_send'] - basetime, unit='ms')
    df = df.drop(['time_send', 'time_receive', 'time'], axis=1)

    return df


def main():
    parser = argparse.ArgumentParser(
            formatter_class=argparse.ArgumentDefaultsHelpFormatter
        )
    parser.add_argument('--name', default='', help='Plot name')
    parser.add_argument('--config', default='', help='Use a config file to'
                        ' read metadata such as the basetime')
    parser.add_argument('--capacity', default='', help='Link capacity log')
    parser.add_argument('--rtp-sent', help='Senderside RTP logfile to include'
                        ' in plot')
    parser.add_argument('--rtp-received', help='Receiverside RTP logfile to'
                        ' include in plot')
    parser.add_argument('--rtcp-sent', help='Senderside RTCP logfile to'
                        ' include in plot')
    parser.add_argument('--rtcp-received', help='Receiverside RTCP logfile to'
                        ' include in plot')
    parser.add_argument('--cc', help='CC file to include in plot')
    parser.add_argument('--loss', nargs=2, help='plot loss between an RTP sent'
                        ' log file and an RTP received log file',
                        metavar=('sent_rtp.log', 'received_rtp.log'))
    parser.add_argument('--latency', nargs=2, help='RTP latency plot between'
                        ' an RTP sent log file and an RTP received log file',
                        metavar=('sent_rtp.log', 'received_rtp.log'))
    parser.add_argument('--qdelay', help='SCReAM queue delay')
    parser.add_argument('-o', '--output', required=True, help='output file')
    parser.add_argument('-b', '--basetime', type=int, help='basetime to use in'
                        ' plots, if not given, will be inferred from the input'
                        ' data using the first row')

    args = parser.parse_args()

    if args.config and not args.basetime:
        with open(args.config) as f:
            d = json.load(f)
            args.basetime = d['basetime']

    print(args)

    fig, ax = plt.subplots(figsize=(8, 2), dpi=400)

    labels = []
    if args.capacity:
        data = read_capacity(
                args.capacity,
                args.basetime,
            )
        labels.append(stepper(ax, data, {
            'label': 'Link Capacity',
            }))
    if args.rtp_sent:
        data = read_rtp(
                args.rtp_sent,
                args.basetime,
            )
        labels.append(plotter(ax, data, {
            'label': 'Sent RTP',
        }))

    if args.rtp_received:
        data = read_rtp(
                args.rtp_received,
                args.basetime,
            )
        labels.append(plotter(ax, data, {
            'label': 'Received RTP',
        }))

    if args.rtcp_sent:
        data = read_rtcp(
                args.rtcp_sent,
                args.basetime,
            )
        labels.append(plotter(ax, data, {
            'label': 'Sent RTCP',
        }))

    if args.rtcp_received:
        data = read_rtcp(
                args.rtcp_received,
                args.basetime,
            )
        labels.append(plotter(ax, data, {
            'label': 'Received RTCP',
        }))

    if args.cc:
        data = read_cc_target_rate(
                args.cc,
                args.basetime,
            )
        labels.append(plotter(ax, data, {
            'label': 'CC Target Bitrate',
        }))

    if args.loss:
        data = read_rtp_loss(
                args.loss[0],
                args.loss[1],
                args.basetime,
            )
        labels.append(plotter(ax, data, {
            'label': 'RTP loss',
        }))

    if args.latency:
        data = read_rtp_latency(
                args.latency[0],
                args.latency[1],
                args.basetime,
            )
        labels.append(scatter(ax, data, {
            'label': 'RTP latency',
            }))

    if args.qdelay:
        data = read_cc_qdelay(
                args.qdelay,
                args.basetime,
            )
        labels.append(plotter(ax, data, {
            'label': 'SCReAM Queue Delay',
        }))

    if args.cc or args.rtp_sent or args.rtp_received:
        ax.set_xlabel('Time')
        ax.set_ylabel('Rate')
        ax.set_title(args.name)
        ax.xaxis.set_major_formatter(mdates.DateFormatter("%M:%S"))
        ax.yaxis.set_major_formatter(EngFormatter(unit='bit/s'))
        ax.set_xlim([dt.datetime(1970, 1, 1), dt.datetime(1970, 1, 1,
                    minute=2)])

    if args.loss:
        ax.set_xlabel('Time')
        ax.set_ylabel('Packet Loss')
        ax.set_title(args.name)
        ax.xaxis.set_major_formatter(mdates.DateFormatter("%M:%S"))
        ax.yaxis.set_major_formatter(PercentFormatter(xmax=1.0))
        ax.set_xlim([dt.datetime(1970, 1, 1), dt.datetime(1970, 1, 1,
                    minute=2)])

    # lgd = ax.legend(handles=labels, loc='upper right', bbox_to_anchor=(1,
    #                 1), ncol=2)
    lgd = ax.legend(handles=labels)
    fig.tight_layout()
    fig.savefig(args.output, bbox_extra_artists=(lgd,), bbox_inches='tight')
    # fig.savefig(args.output)


if __name__ == "__main__":
    main()
